package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// GetMeta returns the value stored for key in the meta table.
//
// The boolean result reports whether the key exists; a missing key is not an
// error so callers can distinguish "unset" from "set to empty".
func GetMeta(ctx context.Context, db *sql.DB, key string) (string, bool, error) {
	var value string
	err := db.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = ?", key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("store: get meta %q: %w", key, err)
	}
	return value, true, nil
}

// SetMeta upserts a key/value pair into the meta table.
func SetMeta(ctx context.Context, db *sql.DB, key, value string) error {
	_, err := db.ExecContext(ctx,
		"INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		key, value)
	if err != nil {
		return fmt.Errorf("store: set meta %q: %w", key, err)
	}
	return nil
}
