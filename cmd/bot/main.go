// Command bot is the growbot entrypoint.
//
// By default it runs the daemon: it learns vocabulary from the local timeline
// and autonomously posts generated sentences. Auxiliary modes are available for
// development:
//
//	-smoke         run a foundational smoke check (morph + optional credentials)
//	-gen           generate one sentence from the current model and print it
//	-post "text"   post a single literal note and exit
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	// 生活リズム(biorhythm)はローカル時刻を使うため、distroless等の最小イメージでも
	// TZ環境変数が効くようタイムゾーンDBをバイナリへ埋め込む。
	_ "time/tzdata"

	"growbot/internal/agent"
	"growbot/internal/config"
	"growbot/internal/connector/misskey"
	"growbot/internal/markov"
	"growbot/internal/morph"
	"growbot/internal/observability"
	"growbot/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

// run parses flags, builds shared dependencies, and dispatches to the selected
// mode.
func run() error {
	smoke := flag.Bool("smoke", false, "run the foundational smoke check and exit")
	gen := flag.Bool("gen", false, "generate one sentence from the current model and exit (no network)")
	postText := flag.String("post", "", "post a single literal note and exit (requires Misskey credentials)")
	sample := flag.String("sample", "すもももももももものうち", "sample sentence for the -smoke morphological-analysis check")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger := observability.NewLogger(cfg.LogLevel, cfg.LogFormat)

	ctx := context.Background()

	// store と morph は全モードで必要。
	db, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	tok, err := morph.New()
	if err != nil {
		return fmt.Errorf("init morph: %w", err)
	}

	// マルコフ次数はDB構築時の値と一致していなければならない。不一致だと生成開始の
	// 文脈キーが既存のprev_keyと一致せず、エラーも出さず空文を生成し続けてしまう。
	// 既存データがある場合はDB側の次数を採用し、設定との差異は警告で知らせる。
	repo := markov.NewSQLRepo(db)
	dbOrder, hasData, err := repo.InferOrder(ctx)
	if err != nil {
		return fmt.Errorf("infer markov order: %w", err)
	}
	order, mismatch := resolveOrder(cfg.MarkovOrder, dbOrder, hasData)
	if mismatch {
		logger.Warn("configured MARKOV_ORDER does not match the existing database; using the database's order to keep generation consistent",
			"configured", cfg.MarkovOrder, "database", dbOrder)
	}

	model := markov.NewModel(
		repo,
		markov.Options{
			Order:     order,
			MinTokens: cfg.MinSentenceTokens,
			MaxTokens: cfg.MaxSentenceTokens,
		},
		nil,
	)

	switch {
	case *smoke:
		return smokeCheck(ctx, cfg, logger, tok, *sample)
	case *gen:
		return generateOnce(ctx, logger, db, tok, model)
	case *postText != "":
		return postLiteral(ctx, cfg, logger, *postText)
	default:
		return runDaemon(cfg, logger, db, tok, model)
	}
}

// resolveOrder picks the n-gram order to use. When the database already holds
// chains it returns the database's own order so generation stays consistent
// with the stored prev_keys; otherwise it returns the configured order. The
// boolean reports a mismatch that is worth warning about.
func resolveOrder(configured, dbOrder int, hasData bool) (order int, mismatch bool) {
	if hasData && dbOrder != configured {
		return dbOrder, true
	}
	return configured, false
}

// smokeCheck verifies morphological analysis works and, when credentials are
// configured, that the Misskey account is reachable.
func smokeCheck(ctx context.Context, cfg *config.Config, logger *slog.Logger, tok *morph.Tokenizer, sample string) error {
	tokens := tok.Tokenize(sample)
	fmt.Printf("morph: %q -> %d tokens\n", sample, len(tokens))
	for _, t := range tokens {
		fmt.Printf("  %s\t[%s]\t%s\n", t.Surface, t.POS, t.BaseForm)
	}

	if cfg.MisskeyBaseURL == "" || cfg.MisskeyToken == "" {
		logger.Warn("Misskey credentials not set; skipping connector smoke (set MISSKEY_BASE_URL and MISSKEY_TOKEN)")
		return nil
	}
	client := misskey.NewClient(cfg.MisskeyBaseURL, cfg.MisskeyToken, nil)
	me, err := client.Me(ctx)
	if err != nil {
		return fmt.Errorf("verify misskey credentials: %w", err)
	}
	logger.Info("misskey credentials ok", "username", me.Username, "id", me.ID)
	fmt.Printf("misskey ok: @%s\n", me.Username)
	return nil
}

// generateOnce seeds the model if needed, then prints a single generated
// sentence. It performs no network I/O so it works offline.
func generateOnce(ctx context.Context, logger *slog.Logger, db *sql.DB, tok *morph.Tokenizer, model *markov.Model) error {
	if err := agent.EnsureSeeded(ctx, db, tok, model, logger); err != nil {
		return fmt.Errorf("ensure seeded: %w", err)
	}
	text, err := model.Generate(ctx)
	if err != nil {
		return fmt.Errorf("generate: %w", err)
	}
	st, _ := model.Stats(ctx)
	fmt.Println(text)
	logger.Info("generated", "len", len(text), "vocab", st.Vocab, "chains", st.Chains)
	return nil
}

// postLiteral posts a single literal note and exits.
func postLiteral(ctx context.Context, cfg *config.Config, logger *slog.Logger, text string) error {
	if cfg.MisskeyBaseURL == "" || cfg.MisskeyToken == "" {
		return fmt.Errorf("-post requires MISSKEY_BASE_URL and MISSKEY_TOKEN to be set")
	}
	client := misskey.NewClient(cfg.MisskeyBaseURL, cfg.MisskeyToken, nil)
	id, err := client.CreateNote(ctx, text)
	if err != nil {
		return fmt.Errorf("create note: %w", err)
	}
	logger.Info("note created", "id", id)
	fmt.Printf("posted: note id=%s\n", id)
	return nil
}

// runDaemon runs the autonomous agent until interrupted.
func runDaemon(cfg *config.Config, logger *slog.Logger, db *sql.DB, tok *morph.Tokenizer, model *markov.Model) error {
	if cfg.MisskeyBaseURL == "" || cfg.MisskeyToken == "" {
		return fmt.Errorf("daemon mode requires MISSKEY_BASE_URL and MISSKEY_TOKEN (use -gen or -smoke for offline checks)")
	}

	// SIGINT/SIGTERM で停止できるよう、シグナル連動のコンテキストを使う。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := misskey.NewClient(cfg.MisskeyBaseURL, cfg.MisskeyToken, nil)
	ag := agent.New(cfg, logger, client, tok, model, db)

	logger.Info("growbot daemon starting", "db", cfg.DBPath, "misskey", cfg.MisskeyBaseURL)
	if err := ag.Run(ctx); err != nil {
		return fmt.Errorf("agent run: %w", err)
	}
	logger.Info("growbot daemon stopped")
	return nil
}
