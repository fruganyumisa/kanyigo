package main

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

func OpenDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000;"); err != nil {
		return nil, err
	}
	return db, nil
}

func EnsureSchema(db *sql.DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS maillog_entries (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	ts_utc TEXT NOT NULL,
	host TEXT,
	process TEXT,
	queue_id TEXT,
	mail_from TEXT,
	mail_to TEXT,
	status TEXT,
	relay TEXT,
	delay REAL,
	delays TEXT,
	dsn TEXT,
	message_id TEXT,
	size_bytes INTEGER,
	queued_as TEXT,
	mail_id TEXT,
	subject TEXT,
	hits REAL,
	helo TEXT,
	amavis_origin TEXT,
	raw TEXT NOT NULL,
	raw_hash TEXT NOT NULL UNIQUE
);

CREATE INDEX IF NOT EXISTS idx_maillog_ts ON maillog_entries(ts_utc);
CREATE INDEX IF NOT EXISTS idx_maillog_from ON maillog_entries(mail_from);
CREATE INDEX IF NOT EXISTS idx_maillog_to ON maillog_entries(mail_to);
CREATE INDEX IF NOT EXISTS idx_maillog_status ON maillog_entries(status);
CREATE INDEX IF NOT EXISTS idx_maillog_queue ON maillog_entries(queue_id);

CREATE TABLE IF NOT EXISTS ingest_state (
	key TEXT PRIMARY KEY,
	offset_bytes INTEGER NOT NULL,
	inode INTEGER NOT NULL,
	updated_at TEXT NOT NULL
);
`
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	if err := ensureColumns(db, "maillog_entries", []string{
		"queued_as", "mail_id", "subject", "hits", "helo", "amavis_origin",
	}); err != nil {
		return err
	}
	return nil
}

func ensureColumns(db *sql.DB, table string, cols []string) error {
	existing := map[string]bool{}
	rows, err := db.Query("PRAGMA table_info(" + table + ");")
	if err != nil {
		return fmt.Errorf("table_info: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dfltValue interface{}
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return fmt.Errorf("table_info scan: %w", err)
		}
		existing[name] = true
	}
	colTypes := map[string]string{
		"queued_as":     "TEXT",
		"mail_id":       "TEXT",
		"subject":       "TEXT",
		"hits":          "REAL",
		"helo":          "TEXT",
		"amavis_origin": "TEXT",
	}
	for _, col := range cols {
		if existing[col] {
			continue
		}
		colType := colTypes[col]
		if colType == "" {
			colType = "TEXT"
		}
		if _, err := db.Exec("ALTER TABLE " + table + " ADD COLUMN " + col + " " + colType + ";"); err != nil {
			return fmt.Errorf("add column %s: %w", col, err)
		}
	}
	return nil
}
