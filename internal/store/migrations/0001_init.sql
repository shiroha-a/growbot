-- Single-row identity table for the bot itself.
-- The CHECK (id = 1) constraint enforces that only one self row can ever exist.
CREATE TABLE IF NOT EXISTS self (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  born_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
