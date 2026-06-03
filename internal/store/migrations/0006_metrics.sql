-- Growth metrics snapshots.
-- Phase 6 では bot の成長を定点観測する。metrics は一定間隔で記録した
-- 語彙数(vocab)・連鎖数(chains)・友好関係数(friends)・成長段階(stage)・
-- 平均報酬(avg_reward)のスナップショットを蓄積し、推移を後から分析できるようにする。
-- recorded_at は記録時刻で、未指定時は CURRENT_TIMESTAMP が入る。
CREATE TABLE IF NOT EXISTS metrics (
  recorded_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  vocab       INTEGER NOT NULL DEFAULT 0,
  chains      INTEGER NOT NULL DEFAULT 0,
  friends     INTEGER NOT NULL DEFAULT 0,
  stage       TEXT NOT NULL DEFAULT '',
  avg_reward  REAL NOT NULL DEFAULT 0
);
