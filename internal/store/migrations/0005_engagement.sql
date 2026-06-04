-- Engagement-learning tables.
-- Phase 5 では bot が投稿した内容(posts)と、その反応から得られた報酬を学習する。
-- posts は各投稿の note_id ごとにリアクション/返信/リノートの計測値と
-- そこから算出した reward を保持し、observed で計測済みかどうかを管理する。
-- bandit_arms は Thompson サンプリングの腕ごとに Beta 分布の alpha/beta と
-- 累積統計(pulls, reward_sum)を保持する。
CREATE TABLE IF NOT EXISTS posts (
  note_id     TEXT PRIMARY KEY,
  kind        TEXT NOT NULL DEFAULT '',
  arm         TEXT NOT NULL DEFAULT '',
  text        TEXT NOT NULL DEFAULT '',
  reactions   INTEGER NOT NULL DEFAULT 0,
  replies     INTEGER NOT NULL DEFAULT 0,
  renotes     INTEGER NOT NULL DEFAULT 0,
  reward      REAL NOT NULL DEFAULT 0,
  observed    INTEGER NOT NULL DEFAULT 0,
  created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  observed_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS bandit_arms (
  arm        TEXT PRIMARY KEY,
  alpha      REAL NOT NULL DEFAULT 1.0,
  beta       REAL NOT NULL DEFAULT 1.0,
  pulls      INTEGER NOT NULL DEFAULT 0,
  reward_sum REAL NOT NULL DEFAULT 0
);
