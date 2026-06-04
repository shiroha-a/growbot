// Package store provides the SQLite-backed persistence layer for growbot.
//
// It uses the pure-Go modernc.org/sqlite driver so the binary stays
// dependency-free and fully local with no cgo.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	// 純Goのsqliteドライバ。cgo不要でローカル完結のため採用。
	_ "modernc.org/sqlite"
)

// Open opens (or creates) the SQLite database at path, configures connection
// PRAGMAs, verifies connectivity, and applies any pending migrations.
//
// On any failure after the handle is created, the database is closed before
// the error is returned so callers never leak an open handle.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	// DBファイルの親ディレクトリを先に作る。コンテナでvolumeをマウントした場合は
	// 既存だが、bind mountや初回起動で親が無いケースでも安全に作成できるようにする。
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("store: create db dir %q: %w", dir, err)
		}
	}

	// PRAGMAは接続ごとの設定で、db.Execだとプールのうち1接続にしか効かない。
	// プールが新たに開く接続すべてに適用されるよう、DSNの_pragmaクエリに載せる。
	// _txlock=immediate により書き込みトランザクションが開始時に書き込みロックを
	// 取得するため、複数ライターが競合しても busy_timeout で直列化できる(DEFERRED
	// だと遅延昇格時に即SQLITE_BUSYとなりbusy_timeoutが効かない)。
	dsn := "file:" + path + "?" + url.Values{
		"_pragma": {"busy_timeout=5000", "foreign_keys=ON", "journal_mode=WAL"},
		"_txlock": {"immediate"},
	}.Encode()

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %q: %w", path, err)
	}

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}

	// WALは無音で失敗しうる(WAL非対応のFS等)。実際に適用されたか読み戻して検証する。
	var mode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode;").Scan(&mode); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: read journal_mode: %w", err)
	}
	if !strings.EqualFold(mode, "wal") {
		db.Close()
		return nil, fmt.Errorf("store: journal_mode not WAL, got %q", mode)
	}

	if err := Migrate(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}

	return db, nil
}
