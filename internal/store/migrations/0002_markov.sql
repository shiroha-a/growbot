-- Vocabulary table: one row per observed surface form.
-- freq accumulates how often the token has been learned; last_used drives
-- recency-aware behavior in later phases.
CREATE TABLE IF NOT EXISTS tokens (
  surface   TEXT PRIMARY KEY,
  pos       TEXT NOT NULL DEFAULT '',
  freq      INTEGER NOT NULL DEFAULT 0,
  last_used TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Markov transition table: one row per (context, next-token) pair.
-- count は観測回数。reward_sum/reward_n/strength は後続フェーズの
-- バンディット学習で利用する先行定義で、現フェーズでは既定値のまま据え置く。
CREATE TABLE IF NOT EXISTS chains (
  prev_key     TEXT NOT NULL,
  next_surface TEXT NOT NULL,
  count        INTEGER NOT NULL DEFAULT 0,
  reward_sum   REAL NOT NULL DEFAULT 0,
  reward_n     INTEGER NOT NULL DEFAULT 0,
  strength     REAL NOT NULL DEFAULT 1.0,
  last_used    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (prev_key, next_surface)
);

-- prev_key 単位の候補探索(Nexts)は PRIMARY KEY(prev_key, next_surface) の
-- 先頭列プレフィックスで既に賄えるため、専用索引は設けない(学習ホットパスの
-- 書き込み増幅を避ける)。

-- Generic key/value metadata store for forward-looking bookkeeping.
CREATE TABLE IF NOT EXISTS meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
