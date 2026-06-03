-- Social relationships table.
-- Phase 4 では bot がやり取りした相手(actor)ごとの関係性を記録する。
-- affinity は親密度で [0,1]、interactions は言及/返信回数、
-- reactions_received は相手から受け取ったリアクション回数を表す。
CREATE TABLE IF NOT EXISTS relationships (
  actor_id           TEXT PRIMARY KEY,
  username           TEXT NOT NULL DEFAULT '',
  affinity           REAL NOT NULL DEFAULT 0,
  interactions       INTEGER NOT NULL DEFAULT 0,
  reactions_received INTEGER NOT NULL DEFAULT 0,
  first_seen         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_seen          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
