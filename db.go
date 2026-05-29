package main

import (
	"database/sql"
	"fmt"
	"strconv"
	"time"

	_ "github.com/lib/pq"
)

func OpenDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	db.SetMaxOpenConns(envInt("DB_MAX_OPEN_CONNS", 25))
	db.SetMaxIdleConns(envInt("DB_MAX_IDLE_CONNS", 5))
	db.SetConnMaxLifetime(time.Duration(envInt("DB_CONN_MAX_LIFETIME_MINUTES", 30)) * time.Minute)
	return db, nil
}

func EnsureSchema(db *sql.DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS maillog_entries (
	id BIGSERIAL PRIMARY KEY,
	ts_utc TIMESTAMPTZ NOT NULL,
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
	offset_bytes BIGINT NOT NULL,
	inode BIGINT NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS dashboard_users (
	id BIGSERIAL PRIMARY KEY,
	email TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	role TEXT NOT NULL CHECK (role IN ('admin', 'user')),
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_dashboard_users_role ON dashboard_users(role);

CREATE TABLE IF NOT EXISTS dashboard_sessions (
	token_hash TEXT PRIMARY KEY,
	user_id BIGINT NOT NULL REFERENCES dashboard_users(id) ON DELETE CASCADE,
	expires_at TIMESTAMPTZ NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_dashboard_sessions_user ON dashboard_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_dashboard_sessions_expires ON dashboard_sessions(expires_at);
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

func envInt(key string, def int) int {
	value := envOrDefault(key, "")
	if value == "" {
		return def
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return def
	}
	return parsed
}

func ensureColumns(db *sql.DB, table string, cols []string) error {
	existing := map[string]bool{}
	rows, err := db.Query(`
SELECT column_name
FROM information_schema.columns
WHERE table_schema = current_schema() AND table_name = $1;
`, table)
	if err != nil {
		return fmt.Errorf("table_info: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("table_info scan: %w", err)
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("table_info rows: %w", err)
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
