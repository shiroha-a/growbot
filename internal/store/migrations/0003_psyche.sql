-- Psychological state columns for the single self row.
-- Phase 2 では self テーブルに性格(temperament)・気分(mood)・欲求(drives)・
-- エネルギーを持たせる。既存の self テーブルへ ALTER TABLE で列を追加する。
-- 各列は NOT NULL かつ DEFAULT 付きとし、既存行があっても矛盾なく埋まるようにする。

-- Temperament: born-once fixed character, each in [0,1]. Default 0.5 (neutral).
ALTER TABLE self ADD COLUMN extraversion REAL NOT NULL DEFAULT 0.5;
ALTER TABLE self ADD COLUMN neuroticism REAL NOT NULL DEFAULT 0.5;
ALTER TABLE self ADD COLUMN curiosity REAL NOT NULL DEFAULT 0.5;

-- Mood: fluctuating affective state, each in [-1,1]. Default 0 (flat).
ALTER TABLE self ADD COLUMN mood_valence REAL NOT NULL DEFAULT 0;
ALTER TABLE self ADD COLUMN mood_arousal REAL NOT NULL DEFAULT 0;

-- Energy in [0,1]. Default 1.0 (fully rested at birth).
ALTER TABLE self ADD COLUMN energy REAL NOT NULL DEFAULT 1.0;

-- Drives: current deficit per need, each in [0,1]. Default 0 (no deficit).
ALTER TABLE self ADD COLUMN drive_recognition REAL NOT NULL DEFAULT 0;
ALTER TABLE self ADD COLUMN drive_curiosity REAL NOT NULL DEFAULT 0;
ALTER TABLE self ADD COLUMN drive_expression REAL NOT NULL DEFAULT 0;
ALTER TABLE self ADD COLUMN drive_boredom REAL NOT NULL DEFAULT 0;

-- Last time the psychological state was persisted.
-- 既存行があるテーブルへの ADD COLUMN では非定数 DEFAULT(CURRENT_TIMESTAMP)は
-- SQLite に拒否されるため、定数で追加してから既存行をバックフィルする。
ALTER TABLE self ADD COLUMN updated_at TIMESTAMP NOT NULL DEFAULT '1970-01-01T00:00:00Z';
UPDATE self SET updated_at = CURRENT_TIMESTAMP;
