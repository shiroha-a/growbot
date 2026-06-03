// Package agent wires perception, learning and generation into the bot's
// runtime loop. From Phase 2 the loop is internally driven: instead of posting
// on a fixed timer, the bot accumulates drives, follows a daily sleep/energy
// rhythm, and posts when its aggregate urge crosses a threshold while awake and
// rested. Its mood colours the surface tone of what it says.
package agent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"golang.org/x/sync/errgroup"

	"growbot/internal/assoc"
	"growbot/internal/biorhythm"
	"growbot/internal/config"
	"growbot/internal/connector/misskey"
	"growbot/internal/generation/seed"
	"growbot/internal/generation/voice"
	"growbot/internal/habit"
	"growbot/internal/learning"
	"growbot/internal/lifecycle"
	"growbot/internal/markov"
	"growbot/internal/memory"
	"growbot/internal/metrics"
	"growbot/internal/morph"
	"growbot/internal/psyche"
	"growbot/internal/safety"
	"growbot/internal/social"
	"growbot/internal/store"
)

// seedFlagKey marks in the meta table that the baseline corpus has been learned.
const seedFlagKey = "seeded_v1"

// memoryStrengthCap is the maximum strength a chain can be reinforced to.
const memoryStrengthCap = 1.0

// metaCatchphrase stores the bot's emergent verbal tic (口癖).
const metaCatchphrase = "catchphrase"

// Bandit arms for autonomous posts: "free" generates from scratch, "topical"
// seeds from a frequent noun. The bot learns which earns more engagement.
const (
	armFree    = "free"
	armTopical = "topical"
)

// banditArms is the set the bandit chooses among for autonomous posts.
var banditArms = []string{armFree, armTopical}

// catchphrasePOS lists the parts of speech a verbal tic is drawn from
// (sentence-final particles and interjections).
var catchphrasePOS = []string{"助詞", "感動詞"}

// learnQueueSize bounds the buffer between the websocket read loop and the
// learn worker, so a slow DB write never blocks ingestion.
const learnQueueSize = 256

// mentionQueueSize bounds the buffer between the main-channel reader and the
// life loop, which handles mentions so all self-state stays single-goroutine.
const mentionQueueSize = 64

// Mention-response dynamics. 構ってもらえた時の承認充足と気分の上向き。
const (
	recognitionSatOnMention = 0.2             // メンションで承認欲求が大きく満たされる
	moodNudgeOnMentionV     = 0.1             // 構ってもらえた嬉しさで valence が上向く
	affinityToneScale       = 0.3             // 親密度を返信トーン(valence)へ反映する強さ
	mentionMinInterval      = 8 * time.Second // 連続返信の最小間隔(rapid-fire/bot間ループの緩和)
	seenMentionsCap         = 512             // 重複排除用に保持する直近note IDの最大数
)

// Stream reconnection backoff bounds.
const (
	reconnectBackoffBase = 5 * time.Second
	reconnectBackoffMax  = 60 * time.Second
	// healthyConnDuration: 接続がこの時間以上継続したら健全とみなしバックオフを基準値へ戻す。
	healthyConnDuration = 30 * time.Second
)

// Internal-drive dynamics applied on each life-loop tick. これらは1tickごとの
// 感情・欲求・エネルギーの増減量で、bot の「気分屋」な挙動を形づくる。
const (
	moodInertiaRate      = 0.1  // 気分が基線へ戻る割合(慣性)
	curiositySatPerNote  = 0.05 // 新規学習1件あたりの好奇心充足
	boredomReliefSeen    = 0.1  // TLに動きがあった時の退屈緩和
	expressionSatOnPost  = 0.5  // 発話による表現欲の充足
	boredomReliefOnPost  = 0.3  // 発話による退屈の解消
	recognitionSatOnPost = 0.05 // 発話自体の僅かな承認充足(本来の充足はPhase 3の反応で)
	actEnergyCost        = 0.05 // 発話のエネルギー消費
	minEnergyToAct       = 0.15 // 発話に必要な最低エネルギー
	moodNudgeOnActV      = 0.08 // 行動できた安堵で valence が少し上向く
	moodNudgeOnActA      = 0.04 // 同 arousal
)

// EnsureSeeded learns the baseline seed corpus exactly once, so the bot is
// "born at a certain level" rather than mute.
//
// Idempotency relies on the seeded flag being persisted after learning. If the
// process crashes between learning the corpus and setting the flag, the small
// seed corpus is re-learned on the next start (slightly inflating those counts);
// 連続学習で薄まるため許容している。
func EnsureSeeded(ctx context.Context, db *sql.DB, tok *morph.Tokenizer, model *markov.Model, logger *slog.Logger) error {
	_, seeded, err := store.GetMeta(ctx, db, seedFlagKey)
	if err != nil {
		return err
	}
	if seeded {
		return nil
	}

	sentences := seed.Sentences()
	for _, s := range sentences {
		tokens := tok.Tokenize(s)
		if len(tokens) == 0 {
			continue
		}
		if err := model.Learn(ctx, tokens); err != nil {
			return fmt.Errorf("learn seed sentence: %w", err)
		}
	}
	if err := store.SetMeta(ctx, db, seedFlagKey, "1"); err != nil {
		return err
	}

	if st, err := model.Stats(ctx); err != nil {
		logger.Warn("seed stats failed", "err", err)
	} else {
		logger.Info("seeded baseline vocabulary", "sentences", len(sentences), "vocab", st.Vocab, "chains", st.Chains)
	}
	return nil
}

// Agent owns the runtime loop and its dependencies.
type Agent struct {
	cfg     *config.Config
	logger  *slog.Logger
	client  *misskey.Client
	tok     *morph.Tokenizer
	model   *markov.Model
	db      *sql.DB
	psyche  *psyche.Store
	social  *social.Store
	bandit  *learning.Bandit
	ledger  *learning.Ledger
	metrics *metrics.Store
	guard   *safety.Guard
	limiter *safety.RateLimiter
	clock   biorhythm.Clock
	rng     *rand.Rand

	// selfID is the bot's own account ID, used to ignore its own notes.
	selfID string
	// stage is the current developmental stage (life-loop goroutine only).
	stage lifecycle.Stage
	// catchphrase is the bot's emergent verbal tic (life-loop goroutine only).
	catchphrase string
	// self is the in-memory psychological state. It is mutated only by the life
	// loop goroutine (both ticks and mention handling), so it needs no lock.
	self *psyche.Self
	// lastPost is when the bot last spoke autonomously.
	lastPost time.Time
	// wasAsleep tracks the previous tick's sleep state to detect transitions.
	wasAsleep bool

	// learnChannel is the Misskey streaming timeline the bot learns from,
	// derived from cfg.LearnTimeline.
	learnChannel string
	// learnCh decouples websocket ingestion from the (DB-bound) learning step.
	learnCh chan misskey.StreamNote
	// mentionCh hands mentions to the life loop so mention handling and ticks
	// mutate self on the same goroutine (no lock needed).
	mentionCh chan misskey.StreamNote
	// seenMentions de-duplicates a note delivered as both "mention" and "reply".
	// Accessed only by the mention-loop goroutine (enqueueMention).
	seenMentions map[string]struct{}
	seenOrder    []string
	// lastReply throttles mention replies (life-loop goroutine only).
	lastReply time.Time
	// notesSeen / notesLearned are sampled each tick to gauge timeline activity
	// and novelty. これらは別ゴルーチンから加算されるためアトミックに扱う。
	notesSeen    atomic.Int64
	notesLearned atomic.Int64
}

// New constructs an Agent from its dependencies.
func New(cfg *config.Config, logger *slog.Logger, client *misskey.Client, tok *morph.Tokenizer, model *markov.Model, db *sql.DB) *Agent {
	return &Agent{
		cfg:          cfg,
		logger:       logger,
		client:       client,
		tok:          tok,
		model:        model,
		db:           db,
		psyche:       psyche.NewStore(db),
		social:       social.NewStore(db),
		bandit:       learning.NewBandit(db, rand.New(rand.NewSource(time.Now().UnixNano()))),
		ledger:       learning.NewLedger(db),
		metrics:      metrics.NewStore(db),
		guard:        safety.NewGuard(cfg.NGWords, cfg.MaxSymbolRatio),
		limiter:      safety.NewRateLimiter(cfg.MaxPostsPerHour, time.Hour),
		clock:        biorhythm.Clock{SleepStartHour: cfg.SleepStartHour, SleepEndHour: cfg.SleepEndHour},
		rng:          rand.New(rand.NewSource(time.Now().UnixNano())),
		learnChannel: timelineChannel(cfg.LearnTimeline),
		learnCh:      make(chan misskey.StreamNote, learnQueueSize),
		mentionCh:    make(chan misskey.StreamNote, mentionQueueSize),
		seenMentions: make(map[string]struct{}, seenMentionsCap),
	}
}

// Run seeds the model, resolves identity, births/loads the psychological state,
// and runs the stream, learn and life loops until ctx is canceled.
func (a *Agent) Run(ctx context.Context) error {
	if err := EnsureSeeded(ctx, a.db, a.tok, a.model, a.logger); err != nil {
		return fmt.Errorf("ensure seeded: %w", err)
	}

	// 自分の投稿を学習対象から外し、bot間/自己ループを防ぐため自身のIDを把握する。
	me, err := a.client.Me(ctx)
	if err != nil {
		return fmt.Errorf("resolve self account: %w", err)
	}
	a.selfID = me.ID

	self, err := a.psyche.LoadOrBirth(ctx, a.rng)
	if err != nil {
		return fmt.Errorf("load self: %w", err)
	}
	a.self = self

	// 口癖(あれば)を読み込み、誕生時点の発達段階を反映する。
	if cp, ok, err := store.GetMeta(ctx, a.db, metaCatchphrase); err == nil && ok {
		a.catchphrase = cp
	}
	a.applyStage(ctx)

	a.logger.Info("agent starting",
		"self", me.Username,
		"born_at", self.BornAt.Format(time.RFC3339),
		"extraversion", self.Temperament.Extraversion,
		"neuroticism", self.Temperament.Neuroticism,
		"curiosity", self.Temperament.Curiosity,
		"energy", self.Energy,
		"stage", a.stage.String(),
		"tick", a.cfg.TickInterval.String(),
		"post_min_interval", a.cfg.PostInterval.String(),
		"urge_threshold", a.cfg.UrgeThreshold,
		"autonomous_post", a.cfg.AutonomousPost,
		"learn_timeline", a.learnChannel,
	)

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { a.streamLoop(gctx); return nil })
	g.Go(func() error { a.learnWorker(gctx); return nil })
	g.Go(func() error { a.mentionLoop(gctx); return nil })
	g.Go(func() error { a.engagementLoop(gctx); return nil })
	g.Go(func() error { a.lifeLoop(gctx); return nil })
	return g.Wait()
}

// timelineChannel maps a LEARN_TIMELINE config value to its Misskey streaming
// channel name. The value is validated by config, so an unknown value falls
// back to the local timeline rather than failing.
func timelineChannel(name string) string {
	switch name {
	case "global":
		return "globalTimeline"
	case "hybrid":
		return "hybridTimeline"
	case "home":
		return "homeTimeline"
	default: // "local"
		return "localTimeline"
	}
}

// japaneseRatio returns the fraction of Japanese characters among the non-space
// runes of s (0 for an empty or all-space string). It is used to skip learning
// from predominantly non-Japanese notes on busy, multilingual timelines.
func japaneseRatio(s string) float64 {
	var jp, total int
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		total++
		if isJapanese(r) {
			jp++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(jp) / float64(total)
}

// isJapanese reports whether r is a hiragana, katakana, or kanji character
// (including the long-vowel mark and halfwidth katakana).
func isJapanese(r rune) bool {
	switch {
	case r >= 0x3040 && r <= 0x309F: // hiragana
		return true
	case r >= 0x30A0 && r <= 0x30FF: // katakana (includes ー, U+30FC)
		return true
	case r >= 0x4E00 && r <= 0x9FFF: // CJK unified ideographs (kanji)
		return true
	case r >= 0x3400 && r <= 0x4DBF: // CJK extension A
		return true
	case r >= 0xFF66 && r <= 0xFF9D: // halfwidth katakana
		return true
	default:
		return false
	}
}

// streamLoop subscribes to the configured learn timeline and re-subscribes with
// a capped exponential backoff whenever the stream drops, until ctx is canceled.
func (a *Agent) streamLoop(ctx context.Context) {
	backoff := reconnectBackoffBase
	for {
		if ctx.Err() != nil {
			return
		}
		start := time.Now()
		err := a.client.StreamTimeline(ctx, a.learnChannel, a.enqueueNote)
		if ctx.Err() != nil {
			return
		}
		// 接続が一定時間続いたなら健全とみなしバックオフをリセットする。
		if time.Since(start) >= healthyConnDuration {
			backoff = reconnectBackoffBase
		}
		if err != nil {
			a.logger.Warn("stream disconnected; reconnecting", "err", err, "backoff", backoff.String())
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > reconnectBackoffMax {
			backoff = reconnectBackoffMax
		}
	}
}

// enqueueNote filters and hands a received note to the learn worker without
// blocking the websocket read loop. A full queue drops the note (logged).
func (a *Agent) enqueueNote(note misskey.StreamNote) {
	if note.UserID == a.selfID {
		return
	}
	if strings.TrimSpace(note.Text) == "" {
		return
	}
	// タイムラインの活発さを測るため、学習対象になりうるノートを数える。
	a.notesSeen.Add(1)
	select {
	case a.learnCh <- note:
	default:
		// 学習が追いつかない場合はノートを捨てる(読み取りループは止めない)。
		a.logger.Warn("learn queue full; dropping note")
	}
}

// mentionLoop subscribes to the main channel for mentions and re-subscribes with
// a capped exponential backoff whenever the stream drops, until ctx is canceled.
func (a *Agent) mentionLoop(ctx context.Context) {
	backoff := reconnectBackoffBase
	for {
		if ctx.Err() != nil {
			return
		}
		start := time.Now()
		err := a.client.StreamMain(ctx, a.enqueueMention)
		if ctx.Err() != nil {
			return
		}
		if time.Since(start) >= healthyConnDuration {
			backoff = reconnectBackoffBase
		}
		if err != nil {
			a.logger.Warn("mention stream disconnected; reconnecting", "err", err, "backoff", backoff.String())
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > reconnectBackoffMax {
			backoff = reconnectBackoffMax
		}
	}
}

// enqueueMention hands a mention to the life loop (skipping the bot's own notes
// and duplicates) without blocking the websocket read loop.
func (a *Agent) enqueueMention(note misskey.StreamNote) {
	if note.UserID == a.selfID {
		return
	}
	// @付きリプライは "mention" と "reply" の2イベントで届くため、note IDで重複排除する。
	if a.alreadySeen(note.ID) {
		return
	}
	select {
	case a.mentionCh <- note:
	default:
		a.logger.Warn("mention queue full; dropping mention")
	}
}

// alreadySeen reports whether a note ID was recently processed, recording it
// otherwise. It is a bounded FIFO touched only by the mention-loop goroutine.
func (a *Agent) alreadySeen(id string) bool {
	if id == "" {
		return false
	}
	if _, ok := a.seenMentions[id]; ok {
		return true
	}
	a.seenMentions[id] = struct{}{}
	a.seenOrder = append(a.seenOrder, id)
	if len(a.seenOrder) > seenMentionsCap {
		oldest := a.seenOrder[0]
		a.seenOrder = a.seenOrder[1:]
		delete(a.seenMentions, oldest)
	}
	return false
}

// handleMention records the relationship with the mentioner and, when enabled,
// replies with a generated note steered toward a keyword from their message and
// toned by how close the bot feels to them. It runs on the life-loop goroutine.
func (a *Agent) handleMention(ctx context.Context, note misskey.StreamNote) {
	if note.UserID == a.selfID {
		return
	}
	rel, err := a.social.Observe(ctx, note.UserID, note.Username, social.KindInteraction, a.cfg.AffinityGain)
	if err != nil {
		a.logger.Warn("observe relationship failed", "err", err)
		return
	}
	if !a.cfg.MentionReply {
		a.logger.Debug("mention noted (reply disabled)", "actor", note.Username)
		return
	}
	// bot同士の無限ループを避けるため、botアカウントには返信しない(関係は記録済み)。
	if note.IsBot {
		a.logger.Debug("mention from bot; not replying", "actor", note.Username)
		return
	}
	// 睡眠中・低エネルギー・直前に返信したばかりなら、休息/レート制限として返信を控える。
	if !shouldReply(a.clock.Asleep(time.Now()), a.self.Energy, time.Since(a.lastReply)) {
		a.logger.Debug("mention noted (resting/throttled)", "actor", note.Username)
		return
	}

	// メンション本文の名詞をseedにして、話題が噛み合う応答を狙う。
	tokens := a.tok.Tokenize(strings.TrimSpace(note.Text))
	seedWord, _ := assoc.PickSeed(tokens, a.rng)
	reply, err := a.model.GenerateFromSeed(ctx, seedWord)
	if err != nil {
		a.logger.Warn("reply generate failed", "err", err)
		return
	}
	// seed単体しか出ない/空なら通常生成にフォールバックする。
	if reply == "" || reply == seedWord {
		reply, err = a.model.Generate(ctx)
		if err != nil {
			a.logger.Warn("reply generate failed", "err", err)
			return
		}
	}
	if reply == "" {
		a.logger.Debug("nothing to reply yet")
		return
	}
	if !a.allowedToPost(reply, time.Now()) {
		return
	}

	// 親密度が高いほど明るいトーンで返す(初対面は中立=控えめ)。連鎖クレジット用に
	// 装飾前の reply を台帳へ記録する。
	mood := a.self.Mood.Nudge(rel.Affinity*affinityToneScale, 0)
	surface := voice.Decorate(reply, mood, a.rng)

	id, err := a.client.CreateReply(ctx, surface, note.ID)
	if err != nil {
		a.logger.Warn("reply post failed", "err", err)
		return
	}
	a.lastReply = time.Now()
	a.recordPost(ctx, id, "reply", "", reply)

	// 構ってもらえたので承認欲求が満たされ、気分が上向き、少しエネルギーを使う。
	a.self.Drives = a.self.Drives.Bump(psyche.DriveRecognition, -recognitionSatOnMention)
	a.self.Mood = a.self.Mood.Nudge(moodNudgeOnMentionV, 0)
	a.self.Energy = biorhythm.Clamp01(a.self.Energy - actEnergyCost*0.5)
	a.logger.Info("replied", "id", id, "to", note.Username, "affinity", rel.Affinity, "text", surface)
}

// shouldReply reports whether the bot should reply to a mention now: it must be
// awake, rested enough, and past the reply cooldown. (The relationship is still
// recorded when this returns false.)
func shouldReply(asleep bool, energy float64, sinceLastReply time.Duration) bool {
	return !asleep && energy >= minEnergyToAct && sinceLastReply >= mentionMinInterval
}

// learnWorker drains the learn queue until ctx is canceled.
func (a *Agent) learnWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case note := <-a.learnCh:
			a.learn(ctx, note)
		}
	}
}

// learn tokenizes a note and updates the Markov model, skipping notes shorter
// than the configured minimum.
func (a *Agent) learn(ctx context.Context, note misskey.StreamNote) {
	text := strings.TrimSpace(note.Text)
	// NGワード/記号過多のノートは語彙汚染を避けるため学習対象から除外する。
	if !a.guard.Allowed(text) {
		return
	}
	// グローバルTL等は多言語のため、日本語比率が低いノートは学習しない。
	// 日本語IPA辞書では非日本語文が断片化して語彙を汚すのを防ぐ。
	if a.cfg.LearnMinJPRatio > 0 && japaneseRatio(text) < a.cfg.LearnMinJPRatio {
		return
	}
	tokens := a.tok.Tokenize(text)
	if len(tokens) < a.cfg.LearnMinTokens {
		return
	}
	if err := a.model.Learn(ctx, tokens); err != nil {
		a.logger.Warn("learn from note failed", "err", err)
		return
	}
	// 何かを学べたことは好奇心の充足材料になる(tickで反映する)。
	a.notesLearned.Add(1)
	a.logger.Debug("learned from note", "tokens", len(tokens))
}

// lifeLoop advances the bot's internal state on every tick and lets it act when
// its drives, energy and rhythm align.
func (a *Agent) lifeLoop(ctx context.Context) {
	ticker := time.NewTicker(a.cfg.TickInterval)
	defer ticker.Stop()
	last := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			dt := now.Sub(last)
			last = now
			a.tick(ctx, now, dt)
		case note := <-a.mentionCh:
			// メンション応答も同じゴルーチンで処理し、self の状態変更を単一化する。
			a.handleMention(ctx, note)
		}
	}
}

// tick updates energy, mood and drives for the elapsed dt and decides whether
// to act. While asleep the bot only recovers energy and stays silent.
func (a *Agent) tick(ctx context.Context, now time.Time, dt time.Duration) {
	asleep := a.clock.Asleep(now)
	wasAsleep := a.wasAsleep
	a.wasAsleep = asleep

	a.self.Energy = biorhythm.EnergyAfter(a.self.Energy, dt, asleep)
	a.self.Mood = a.self.Mood.TowardBaseline(a.self.Temperament.BaselineMood(), moodInertiaRate)

	// 就寝の瞬間に、その日の記憶を整理(定着・忘却・剪定)する。
	if asleep && !wasAsleep {
		a.consolidate(ctx, now)
	}

	if asleep {
		// 眠っている間は発話せず休息する。活動カウンタは消費せず次の覚醒tickへ持ち越す。
		a.persist(ctx)
		a.logger.Debug("asleep", "energy", a.self.Energy)
		return
	}

	// 起床の瞬間に、たまに夢を語る。
	if wasAsleep {
		a.maybeDream(ctx, now)
	}

	// 発達段階を更新し、文長ゲートや口癖の創発を反映する。
	a.applyStage(ctx)

	// 覚醒中のみTL活動・新規学習のカウンタを読み取ってリセットする。
	seen := a.notesSeen.Swap(0)
	learned := a.notesLearned.Swap(0)

	// 起きている間は欲求が時間とともに溜まる。
	a.self.Drives = a.self.Drives.Grow(dt, a.self.Temperament)
	if learned > 0 {
		a.self.Drives = a.self.Drives.Bump(psyche.DriveCuriosity, -curiositySatPerNote*float64(learned))
	}
	if seen > 0 {
		a.self.Drives = a.self.Drives.Bump(psyche.DriveBoredom, -boredomReliefSeen)
	}

	act := decideAct(a.cfg, a.self, now.Sub(a.lastPost))
	a.logger.Debug("tick",
		"urge", a.self.Drives.Urge(),
		"energy", a.self.Energy,
		"valence", a.self.Mood.Valence,
		"arousal", a.self.Mood.Arousal,
		"asleep", asleep,
		"act", act,
	)
	if act {
		a.act(ctx, now)
	}
	a.persist(ctx)
}

// decideAct reports whether the bot should speak now. It is pure so the policy
// can be unit-tested without the heavy dependencies.
func decideAct(cfg *config.Config, self *psyche.Self, sinceLastPost time.Duration) bool {
	return cfg.AutonomousPost &&
		self.Energy >= minEnergyToAct &&
		self.Drives.Urge() >= cfg.UrgeThreshold &&
		sinceLastPost >= cfg.PostInterval
}

// act picks a posting strategy with the bandit, generates a sentence, applies
// the catchphrase and mood tone, posts it, records it for engagement scoring,
// and folds the consequences back into drives, energy and mood.
func (a *Agent) act(ctx context.Context, now time.Time) {
	arm := armFree
	if selected, err := a.bandit.Select(ctx, banditArms); err == nil {
		arm = selected
	} else {
		a.logger.Warn("bandit select failed", "err", err)
	}

	text, effectiveArm, err := a.generateForArm(ctx, arm)
	if err != nil {
		// 生成失敗時もlastPostを進め、毎tick再試行で空回りしないようにする。
		a.lastPost = now
		a.logger.Warn("generate failed", "err", err)
		return
	}
	if text == "" {
		// 語彙が乏しく何も言えない場合もクールダウンを置き、毎tick生成し続けない。
		a.lastPost = now
		a.logger.Debug("nothing to say yet")
		return
	}

	// 安全(NGワード/記号過多)とレート上限のゲート。
	if !a.allowedToPost(text, now) {
		a.lastPost = now
		return
	}

	// 連鎖クレジットは装飾前の素のtextで行うため、台帳にはtextを記録する。
	// 投稿には口癖と気分のトーンを付与する。
	surface := habit.Apply(text, a.catchphrase, a.cfg.CatchphraseChance, a.rng)
	surface = voice.Decorate(surface, a.self.Mood, a.rng)

	id, err := a.client.CreateNote(ctx, surface)
	if err != nil {
		// 投稿失敗時もクールダウンを置き、毎tick再試行でレート枠を浪費しない。
		a.lastPost = now
		a.logger.Warn("post failed", "err", err)
		return
	}
	a.lastPost = now
	// 実際に使われた arm を記録し、バンディットの帰属を正確にする。
	a.recordPost(ctx, id, "autonomous", effectiveArm, text)

	// 発話で表現欲・退屈・承認が満たされ、エネルギーを消費し、気分が少し上向く。
	a.self.Drives = a.self.Drives.Bump(psyche.DriveExpression, -expressionSatOnPost)
	a.self.Drives = a.self.Drives.Bump(psyche.DriveBoredom, -boredomReliefOnPost)
	a.self.Drives = a.self.Drives.Bump(psyche.DriveRecognition, -recognitionSatOnPost)
	a.self.Energy = biorhythm.Clamp01(a.self.Energy - actEnergyCost)
	a.self.Mood = a.self.Mood.Nudge(moodNudgeOnActV, moodNudgeOnActA)

	a.logger.Info("posted", "id", id, "arm", effectiveArm, "text", surface)
}

// generateForArm produces text per the chosen bandit arm and returns the arm
// actually used: "topical" seeds from a frequent noun, "free" generates from
// scratch. When no topical seed is available it falls back to "free" and reports
// "free" so the bandit credit is attributed correctly.
func (a *Agent) generateForArm(ctx context.Context, arm string) (string, string, error) {
	if arm == armTopical {
		if seedWord, ok, err := a.model.FrequentToken(ctx, "名詞", a.cfg.TopicMinFreq); err == nil && ok {
			text, err := a.model.GenerateFromSeed(ctx, seedWord)
			if err != nil {
				return "", armTopical, err
			}
			if text != "" && text != seedWord {
				return text, armTopical, nil
			}
		}
	}
	text, err := a.model.Generate(ctx)
	return text, armFree, err
}

// recordPost adds a post to the engagement ledger (best effort).
func (a *Agent) recordPost(ctx context.Context, noteID, kind, arm, text string) {
	if err := a.ledger.Record(ctx, noteID, kind, arm, text); err != nil {
		a.logger.Warn("record post failed", "err", err)
	}
}

// applyStage recomputes the developmental stage from age and vocabulary, gates
// generation length, and on a stage-up derives a catchphrase and logs the
// milestone. Runs on the life-loop goroutine.
func (a *Agent) applyStage(ctx context.Context) {
	// 成熟に達したら段階は変わらないので、毎tickのStats全走査を避けて短絡する。
	if a.stage == lifecycle.StageMature {
		return
	}
	vocab := 0
	if st, err := a.model.Stats(ctx); err != nil {
		a.logger.Warn("stage stats failed", "err", err)
	} else {
		vocab = st.Vocab
	}
	stage := lifecycle.Evaluate(time.Since(a.self.BornAt), vocab)
	params := stage.Params()
	a.model.SetBounds(params.MinTokens, params.MaxTokens)

	if stage == a.stage {
		return
	}
	grew := stage > a.stage
	a.stage = stage
	a.logger.Info("developmental stage", "stage", stage.String(), "vocab", vocab, "max_tokens", params.MaxTokens)
	if grew && a.catchphrase == "" && stage >= lifecycle.StageAdolescent {
		a.adoptCatchphrase(ctx)
	}
}

// adoptCatchphrase derives an emergent verbal tic from the bot's frequent
// particles/interjections and persists it.
func (a *Agent) adoptCatchphrase(ctx context.Context) {
	candidates := make([]string, 0, len(catchphrasePOS))
	for _, pos := range catchphrasePOS {
		if w, ok, err := a.model.FrequentToken(ctx, pos, a.cfg.TopicMinFreq); err == nil && ok {
			candidates = append(candidates, w)
		}
	}
	cp, ok := habit.PickCatchphrase(candidates, a.rng)
	if !ok {
		return
	}
	a.catchphrase = cp
	if err := store.SetMeta(ctx, a.db, metaCatchphrase, cp); err != nil {
		a.logger.Warn("save catchphrase failed", "err", err)
	}
	a.logger.Info("adopted catchphrase", "catchphrase", cp)
}

// engagementLoop periodically scores the bot's posts by their engagement and
// feeds the reward into the bandit and the chains' reward credit.
func (a *Agent) engagementLoop(ctx context.Context) {
	ticker := time.NewTicker(a.cfg.EngagementPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.scoreEngagement(ctx)
		}
	}
}

// scoreEngagement observes posts old enough to have accrued reactions, computes
// their reward, and updates the bandit and chain credit.
func (a *Agent) scoreEngagement(ctx context.Context) {
	now := time.Now()
	before := now.Add(-a.cfg.ReactionDelay)
	after := now.Add(-a.cfg.EngagementGiveup)
	posts, err := a.ledger.Unobserved(ctx, before, after, 32)
	if err != nil {
		a.logger.Warn("list unobserved posts failed", "err", err)
		return
	}
	for _, p := range posts {
		eng, err := a.client.ShowNote(ctx, p.NoteID)
		if err != nil {
			if errors.Is(err, misskey.ErrNoteNotFound) {
				// 削除済み等で恒久的に取得できないノートは0点で観測済みにし、再試行を止める。
				if merr := a.ledger.MarkObserved(ctx, p.NoteID, 0, 0, 0, 0); merr != nil {
					a.logger.Warn("retire missing note failed", "err", merr)
				}
				continue
			}
			a.logger.Warn("show note failed", "err", err, "note", p.NoteID)
			continue
		}
		reward := learning.Reward(eng.Reactions, eng.Replies, eng.Renotes)
		if err := a.ledger.MarkObserved(ctx, p.NoteID, eng.Reactions, eng.Replies, eng.Renotes, reward); err != nil {
			a.logger.Warn("mark observed failed", "err", err)
			continue
		}
		// 自律投稿のarmはバンディットへ反映し、全投稿の連鎖には報酬をクレジットする。
		if p.Arm == armFree || p.Arm == armTopical {
			if err := a.bandit.Update(ctx, p.Arm, reward); err != nil {
				a.logger.Warn("bandit update failed", "err", err)
			}
		}
		if tokens := a.tok.Tokenize(p.Text); len(tokens) > 0 {
			if err := a.model.Credit(ctx, tokens, reward); err != nil {
				a.logger.Warn("credit chains failed", "err", err)
			}
		}
		a.logger.Info("scored post", "note", p.NoteID, "arm", p.Arm,
			"reactions", eng.Reactions, "replies", eng.Replies, "renotes", eng.Renotes, "reward", reward)
	}
}

// persist saves the current self state, logging (but not failing) on error.
func (a *Agent) persist(ctx context.Context) {
	if err := a.psyche.Save(ctx, a.self); err != nil {
		a.logger.Warn("save self failed", "err", err)
	}
}

// allowedToPost gates a post on content safety and the hourly rate limit. The
// rate slot is consumed only when the content passes the safety check.
func (a *Agent) allowedToPost(text string, now time.Time) bool {
	if !a.guard.Allowed(text) {
		a.logger.Warn("suppressed unsafe post")
		return false
	}
	if !a.limiter.Allow(now) {
		a.logger.Debug("post rate limited")
		return false
	}
	return true
}

// recordMetrics snapshots the bot's growth (vocabulary, chains, friends, stage,
// average reward) for later inspection. Best effort.
func (a *Agent) recordMetrics(ctx context.Context) {
	st, err := a.model.Stats(ctx)
	if err != nil {
		// Stats が失敗すると語彙0の偽スナップショット(記憶喪失)になるため記録を中止する。
		a.logger.Warn("metrics stats failed", "err", err)
		return
	}
	friends, err := a.social.Count(ctx, a.cfg.FriendAffinity)
	if err != nil {
		a.logger.Warn("metrics friends failed", "err", err)
	}
	avg, err := a.ledger.AvgReward(ctx)
	if err != nil {
		a.logger.Warn("metrics avg reward failed", "err", err)
	}
	snap := metrics.Snapshot{
		Vocab:     st.Vocab,
		Chains:    st.Chains,
		Friends:   friends,
		Stage:     a.stage.String(),
		AvgReward: avg,
	}
	if err := a.metrics.Record(ctx, snap); err != nil {
		a.logger.Warn("record metrics failed", "err", err)
	}
	a.logger.Info("growth metrics",
		"vocab", snap.Vocab, "chains", snap.Chains, "friends", snap.Friends,
		"stage", snap.Stage, "avg_reward", snap.AvgReward)
}

// consolidate runs a sleep-time memory pass (reinforce/forget/prune), throttled
// by ConsolidateMinInterval so it happens about once per night.
func (a *Agent) consolidate(ctx context.Context, now time.Time) {
	since, ok, err := memory.LastConsolidation(ctx, a.db)
	if err != nil {
		a.logger.Warn("read last consolidation failed", "err", err)
		return
	}
	// 直近に整理済みなら間隔を空ける(初回は記録が無いので必ず実行する)。
	if ok && now.Sub(since) < a.cfg.ConsolidateMinInterval {
		return
	}
	// Consolidate は強度変更と整理時刻の記録を同一トランザクションで行う。
	report, err := memory.Consolidate(ctx, a.db, since, now, memory.Params{
		Decay:      a.cfg.MemoryDecay,
		Reinforce:  a.cfg.MemoryReinforce,
		Cap:        memoryStrengthCap,
		PruneBelow: a.cfg.MemoryPruneBelow,
	})
	if err != nil {
		a.logger.Warn("consolidate failed", "err", err)
		return
	}
	a.logger.Info("memory consolidated",
		"reinforced", report.Reinforced, "decayed", report.Decayed, "pruned", report.Pruned)

	// 夜ごとに成長指標のスナップショットを記録する。
	a.recordMetrics(ctx)
}

// maybeDream occasionally posts a dream (a surreal recombination of memories) on
// waking, governed by DreamChance.
func (a *Agent) maybeDream(ctx context.Context, now time.Time) {
	if !a.cfg.AutonomousPost {
		return
	}
	// 夢も通常発話と同じく、休息(エネルギー)と投稿間隔のクールダウンに従う。
	if a.self.Energy < minEnergyToAct || now.Sub(a.lastPost) < a.cfg.PostInterval {
		return
	}
	if a.rng.Float64() >= a.cfg.DreamChance {
		return
	}
	text, err := a.model.Dream(ctx)
	if err != nil {
		a.logger.Warn("dream failed", "err", err)
		return
	}
	if text == "" {
		return
	}
	if !a.allowedToPost(text, now) {
		return
	}
	surface := voice.Decorate(text, a.self.Mood, a.rng)
	id, err := a.client.CreateNote(ctx, surface)
	if err != nil {
		a.logger.Warn("dream post failed", "err", err)
		return
	}
	a.lastPost = now
	a.recordPost(ctx, id, "dream", "", text)
	// 夢を語ると表現欲が少し満たされ、わずかにエネルギーを使う。
	a.self.Drives = a.self.Drives.Bump(psyche.DriveExpression, -expressionSatOnPost*0.5)
	a.self.Energy = biorhythm.Clamp01(a.self.Energy - actEnergyCost*0.5)
	a.logger.Info("dreamed", "id", id, "text", surface)
}
