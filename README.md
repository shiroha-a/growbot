# growbot

自前のMisskeyインスタンス上で**生活し、育っていく一個の生き物**を目指した、完全ローカル・LLM非依存のbotです。

LLMやクラウドAPIを一切使わず、人工無脳(マルコフ連鎖)を核に、欲求・感情・生活リズム・記憶・人間関係・発達・強化学習を積み重ねることで、「賢い会話」ではなく「**観察して情が湧く生き物らしさ**」を目指します。

## 特徴

- **完全ローカル**: 外部通信は自前のMisskeyインスタンスへの接続のみ。LLM・埋め込み・外部DB・クラウドサービスは一切使わない。
- **単一バイナリ + SQLite**: `modernc.org/sqlite`(純Go・cgo不要)。状態はすべて`bot.db`に永続化。
- **軽量**: GPU不要、メモリ約100〜250MB(大半は形態素解析辞書)。Raspberry Piでも動く。
- **自動成長**: 人手の調整なしに、語彙・性格・人間関係・方針がオンラインで育つ。

## できること・できないこと

意味の理解・論理的な会話・長い文脈の追跡は**できません**。人間らしさは会話の中身ではなく、気質・生活リズム・記憶の癖・人間関係・発達に宿らせています。完成像は「言葉を覚えたての、気分屋で人懐っこい生き物」です。

## 「成長」の仕組み

| 側面 | 仕組み |
| --- | --- |
| 語彙の成長 | ローカルTLを形態素解析(kagome)し、N-gram遷移を蓄積 |
| 意思 | 欲求(承認/好奇心/表現/退屈)が時間で溜まり、満たそうと自発的に行動 |
| 感情・生活リズム | 気分の波、概日リズム(夜は就寝)、エネルギーの消費と回復 |
| 記憶と忘却 | 睡眠中に記憶を整理(定着・忘却・剪定)、記憶を再結合した「夢」を投稿 |
| 人間関係 | 相手ごとの親密度を覚え、懐き・人見知り |
| 発達 | 段階(少年期→成熟)が進むと文が長く複雑に。口癖が創発 |
| 強化学習 | リアクション等を報酬に、Thompson Samplingで「ウケる投稿方針」を学習。ウケた言い回しの連鎖を生成で優遇 |

## アーキテクチャ

```
cmd/bot ── 起動・モード分岐
  └ internal/agent ── 「生活する」ループ(知覚→学習→意思→生成→投稿→反応学習)
       ├ connector/misskey   REST + Streaming(localTimeline / main)
       ├ morph               形態素解析(kagome)
       ├ generation/markov   マルコフ生成・学習(strength/reward重み付け)
       ├ generation/seed     誕生時の種コーパス
       ├ generation/voice    気分に応じたトーン装飾
       ├ psyche              欲求・感情・気質
       ├ biorhythm           睡眠/覚醒・エネルギー
       ├ memory              睡眠中の記憶整理(忘却曲線)
       ├ social / assoc      人間関係・名詞seed
       ├ lifecycle / habit   発達段階・口癖
       ├ learning            Thompson bandit・投稿台帳・報酬
       ├ safety              NGワード・記号過多・レート制限
       ├ metrics             成長指標の記録
       └ store               SQLite(WAL)+ マイグレーション
```

## ビルド

Go 1.26.3 が必要です(`go.mod`の`toolchain`が自動取得します)。

```sh
go build -o growbot ./cmd/bot
```

## 実行

### オフライン確認(認証情報不要)

```sh
./growbot -smoke           # 形態素解析・DB初期化の疎通確認
./growbot -gen             # 現在のモデルで一文生成して表示
```

### 常駐(自前Misskeyに接続)

`.env`(`configs/.env.example`を参照)に最低限以下を設定:

```sh
MISSKEY_BASE_URL=https://misskey.example.com
MISSKEY_TOKEN=<botアカウントのAPIトークン>
```

```sh
./growbot                  # 既定で常駐(SIGINT/SIGTERMで停止)
./growbot -post "テスト"    # 単発投稿(疎通確認)
```

常駐すると、ローカルTLから語彙を学習し、欲求と生活リズムに従って自発的に発話、メンションに応答し、夜は眠り、睡眠中に記憶を整理し、リアクションから「ウケる投稿」を学習していきます。

### Docker

純Go(cgo不要)なので、マルチステージビルドで最小のdistrolessイメージ(約25MB)になります。

```sh
cp configs/.env.example .env   # MISSKEY_BASE_URL / MISSKEY_TOKEN を埋める
docker compose up -d --build    # ビルドして常駐起動
docker compose logs -f          # ログ確認
docker compose down             # 停止
```

- DBは名前付きvolume`growbot-data`(`/data/bot.db`)に永続化されます。
- 生活リズムはローカル時刻基準のため、`docker-compose.yml`で`TZ=Asia/Tokyo`を設定しています(tzdataはバイナリへ埋め込み済みなので最小イメージでも有効)。
- `.env`はイメージに焼き込まれず、実行時に読み込まれます(トークンを含むため`.dockerignore`で除外)。`.env`未作成でも`compose`自体は起動し、必須値が無ければアプリが明示エラーを出します。
- `MARKOV_ORDER`はDBを構築したときの値と一致させてください。異なる値で起動すると警告を出したうえでDB側の次数を採用します(生成開始の文脈が既存データと一致せず空文になるのを防ぐため)。composeは`.env`の値を使うため通常は自動的に一致します。
- リソース上限(`mem_limit: 512m`/`pids_limit: 256`)を設定済みで、暴走時に母艦を保護します。

`compose`を使わず直接動かす場合(composeと同等の堅牢化フラグを付ける):

```sh
docker build -t growbot .
docker run -d --name growbot --restart unless-stopped \
  --security-opt no-new-privileges=true --cap-drop ALL \
  --memory 512m --pids-limit 256 \
  --env-file .env -e TZ=Asia/Tokyo \
  -v growbot-data:/data growbot
```

#### systemdで常駐(起動時に自動起動)

`docker compose`をsystemdサービスとして管理すると、サーバ再起動後も自動で立ち上がります。ユニット例は`deploy/growbot.service`にあります。

```sh
# リポジトリ(docker-compose.yml と .env を含む)を /opt/growbot に配置してから:
sudo cp deploy/growbot.service /etc/systemd/system/growbot.service
sudo systemctl daemon-reload
sudo systemctl enable --now growbot     # 起動 + 自動起動を有効化
journalctl -u growbot -f                # ログ確認
```

## 設定

すべて環境変数(または`.env`)で調整します。主なもの:

| 変数 | 既定 | 説明 |
| --- | --- | --- |
| `MISSKEY_BASE_URL` / `MISSKEY_TOKEN` | (必須) | 接続先と認証 |
| `DB_PATH` | `./bot.db` | SQLiteファイル |
| `POST_INTERVAL` | `30m` | 自発投稿の最小間隔 |
| `TICK_INTERVAL` | `1m` | 内部状態の更新間隔 |
| `SLEEP_START_HOUR` / `SLEEP_END_HOUR` | `1` / `7` | 睡眠時間帯 |
| `URGE_THRESHOLD` | `0.55` | 発話に必要な衝動の閾値 |
| `LEARN_TIMELINE` | `local` | 学習元TL(`local`/`global`/`hybrid`/`home`)。おひとり様は`global`推奨 |
| `LEARN_MIN_JP_RATIO` | `0.3` | 学習する最低日本語文字比率(多言語TL対策、`0`で無効) |
| `MARKOV_ORDER` | `2` | N-gram階数 |
| `MEMORY_DECAY` / `MEMORY_PRUNE_BELOW` | `0.9` / `0.05` | 忘却・剪定 |
| `DREAM_CHANCE` | `0.3` | 起床時に夢を投稿する確率 |
| `MENTION_REPLY` / `AFFINITY_GAIN` | `true` / `0.1` | メンション応答・親密度 |
| `MENTION_REPLY_ALWAYS` | `false` | `true`で睡眠中・低エネルギーでもメンションに返信(クールダウン・NG・レート上限は維持) |
| `ENGAGEMENT_POLL_INTERVAL` / `REACTION_DELAY` | `5m` / `30m` | 反応回収 |
| `NG_WORDS` / `MAX_SYMBOL_RATIO` / `MAX_POSTS_PER_HOUR` | (空) / `0.5` / `12` | 安全・レート制限 |

`LEARN_TIMELINE=global`(または`hybrid`)は未キュレーションかつ多言語の連合TLから学習するため、不適切表現の学習・出力を抑えたい場合は`NG_WORDS`の整備を推奨します。

全項目は`configs/.env.example`を参照してください。

## テスト

```sh
go test ./...
go test -race ./...
```

## 制約(設計の根幹)

- LLM・外部サービスを一切使わない(自前Misskeyへの接続のみ)。
- 学習・人格更新は全自動(人手承認なし)。
- 状態はローカルSQLiteのみで、単一バイナリで完結。
