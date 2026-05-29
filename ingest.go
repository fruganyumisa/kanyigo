package main

import (
	"bufio"
	"database/sql"
	"os"
	"syscall"
	"time"
)

type IngestStats struct {
	Inserted int
	Skipped  int
	Errors   int
}

func IngestFile(db *sql.DB, path string) (IngestStats, error) {
	file, err := os.Open(path)
	if err != nil {
		return IngestStats{}, err
	}
	defer file.Close()

	loc := time.Local
	now := time.Now()

	stmt, err := db.Prepare(`
INSERT INTO maillog_entries (
	ts_utc, host, process, queue_id, mail_from, mail_to, status, relay, delay, delays, dsn, message_id, size_bytes, queued_as, mail_id, subject, hits, helo, amavis_origin, raw, raw_hash
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)
ON CONFLICT(raw_hash) DO NOTHING;
`)
	if err != nil {
		return IngestStats{}, err
	}
	defer stmt.Close()

	stats := IngestStats{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		entry, ok := ParseLine(line, now, loc)
		if !ok {
			stats.Skipped++
			continue
		}
		res, err := stmt.Exec(
			entry.TSUTC.UTC(),
			entry.Host,
			entry.Process,
			entry.QueueID,
			entry.MailFrom,
			entry.MailTo,
			entry.Status,
			entry.Relay,
			nullFloat(entry.Delay),
			entry.Delays,
			entry.DSN,
			entry.MessageID,
			nullInt64(entry.SizeBytes),
			entry.QueuedAs,
			entry.MailID,
			entry.Subject,
			nullFloat(entry.Hits),
			entry.Helo,
			entry.AmavisOrigin,
			entry.Raw,
			entry.RawHash,
		)
		if err != nil {
			stats.Errors++
			continue
		}
		if rows, err := res.RowsAffected(); err == nil && rows == 0 {
			stats.Skipped++
			continue
		}
		stats.Inserted++
	}
	if err := scanner.Err(); err != nil {
		return stats, err
	}
	return stats, nil
}

func IngestIncremental(db *sql.DB, path string) (IngestStats, error) {
	info, err := os.Stat(path)
	if err != nil {
		return IngestStats{}, err
	}
	inode := fileInode(info)
	size := info.Size()

	state, err := getIngestState(db, path)
	if err != nil {
		return IngestStats{}, err
	}

	offset := int64(0)
	if state.Inode == inode && state.OffsetBytes <= size {
		offset = state.OffsetBytes
	}

	if offset == size {
		return IngestStats{}, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return IngestStats{}, err
	}
	defer file.Close()

	if _, err := file.Seek(offset, 0); err != nil {
		return IngestStats{}, err
	}

	loc := time.Local
	now := time.Now()

	stmt, err := db.Prepare(`
INSERT INTO maillog_entries (
	ts_utc, host, process, queue_id, mail_from, mail_to, status, relay, delay, delays, dsn, message_id, size_bytes, queued_as, mail_id, subject, hits, helo, amavis_origin, raw, raw_hash
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)
ON CONFLICT(raw_hash) DO NOTHING;
`)
	if err != nil {
		return IngestStats{}, err
	}
	defer stmt.Close()

	stats := IngestStats{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		entry, ok := ParseLine(line, now, loc)
		if !ok {
			stats.Skipped++
			continue
		}
		res, err := stmt.Exec(
			entry.TSUTC.UTC(),
			entry.Host,
			entry.Process,
			entry.QueueID,
			entry.MailFrom,
			entry.MailTo,
			entry.Status,
			entry.Relay,
			nullFloat(entry.Delay),
			entry.Delays,
			entry.DSN,
			entry.MessageID,
			nullInt64(entry.SizeBytes),
			entry.QueuedAs,
			entry.MailID,
			entry.Subject,
			nullFloat(entry.Hits),
			entry.Helo,
			entry.AmavisOrigin,
			entry.Raw,
			entry.RawHash,
		)
		if err != nil {
			stats.Errors++
			continue
		}
		if rows, err := res.RowsAffected(); err == nil && rows == 0 {
			stats.Skipped++
			continue
		}
		stats.Inserted++
	}
	if err := scanner.Err(); err != nil {
		return stats, err
	}

	newOffset, err := file.Seek(0, 1)
	if err != nil {
		return stats, err
	}
	if err := setIngestState(db, path, newOffset, inode); err != nil {
		return stats, err
	}
	return stats, nil
}

func nullFloat(v *float64) interface{} {
	if v == nil {
		return nil
	}
	return *v
}

func nullInt64(v *int64) interface{} {
	if v == nil {
		return nil
	}
	return *v
}

type ingestState struct {
	OffsetBytes int64
	Inode       int64
}

func getIngestState(db *sql.DB, key string) (ingestState, error) {
	var state ingestState
	row := db.QueryRow(`SELECT offset_bytes, inode FROM ingest_state WHERE key = $1`, key)
	switch err := row.Scan(&state.OffsetBytes, &state.Inode); err {
	case sql.ErrNoRows:
		return ingestState{OffsetBytes: 0, Inode: 0}, nil
	case nil:
		return state, nil
	default:
		return ingestState{}, err
	}
}

func setIngestState(db *sql.DB, key string, offset int64, inode int64) error {
	_, err := db.Exec(`
INSERT INTO ingest_state (key, offset_bytes, inode, updated_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT(key) DO UPDATE SET offset_bytes=excluded.offset_bytes, inode=excluded.inode, updated_at=excluded.updated_at;
`, key, offset, inode, time.Now().UTC())
	return err
}

func fileInode(info os.FileInfo) int64 {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return int64(stat.Ino)
	}
	return 0
}
