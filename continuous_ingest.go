package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"time"
)

type parsedRecord struct {
	record lineRecord
	entry  *LogEntry
	ok     bool
}

func RunMailLogIngestor(ctx context.Context, db *sql.DB, cfg StreamConfig) error {
	stateKey := continuousIngestStateKey(cfg.Path)
	state, err := getIngestState(db, stateKey)
	if err != nil {
		return fmt.Errorf("load ingest checkpoint: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	records := make(chan lineRecord, 1024)
	parsedRecords := make(chan parsedRecord, 1024)
	followErrors := make(chan error, 1)
	go func() {
		followErrors <- followMailLog(ctx, cfg, state, records)
	}()
	for range cfg.ProcessingWorkers {
		go parseMailLogRecords(ctx, records, parsedRecords)
	}

	stitcher := NewTransactionStitcher(cfg.QueueIdleTimeout)
	stdout := json.NewEncoder(os.Stdout)
	pending := make(map[int64]parsedRecord)
	var nextSequence int64
	evictionInterval := minDuration(cfg.QueueIdleTimeout/2, time.Minute)
	if evictionInterval <= 0 {
		evictionInterval = time.Nanosecond
	}
	evictionTicker := time.NewTicker(evictionInterval)
	defer evictionTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-followErrors:
			if err == nil || errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		case parsed := <-parsedRecords:
			pending[parsed.record.Sequence] = parsed
			for {
				next, ok := pending[nextSequence]
				if !ok {
					break
				}
				delete(pending, nextSequence)
				if err := processParsedRecord(db, stdout, stateKey, stitcher, next); err != nil {
					return err
				}
				nextSequence++
			}
		case now := <-evictionTicker.C:
			for _, transaction := range stitcher.EvictExpired(now) {
				if err := stdout.Encode(transaction); err != nil {
					return fmt.Errorf("encode expired mail transaction: %w", err)
				}
			}
			if err := evictExpiredTransactions(db, now.Add(-cfg.QueueIdleTimeout)); err != nil {
				return err
			}
		}
	}
}

func parseMailLogRecords(ctx context.Context, records <-chan lineRecord, output chan<- parsedRecord) {
	for {
		select {
		case <-ctx.Done():
			return
		case record := <-records:
			entry, ok := ParseLine(record.Text, time.Now(), time.Local)
			select {
			case <-ctx.Done():
				return
			case output <- parsedRecord{record: record, entry: entry, ok: ok}:
			}
		}
	}
}

func processParsedRecord(db *sql.DB, stdout *json.Encoder, stateKey string, stitcher *TransactionStitcher, parsed parsedRecord) error {
	if !parsed.ok {
		log.Printf("skip malformed maillog line at inode=%d offset=%d", parsed.record.Inode, parsed.record.Offset)
	}
	if err := persistMailLogRecord(db, stateKey, parsed.record, parsed.entry); err != nil {
		return err
	}
	if parsed.ok {
		if transaction, complete := stitcher.Apply(parsed.entry); complete {
			if err := stdout.Encode(transaction); err != nil {
				return fmt.Errorf("encode mail transaction: %w", err)
			}
		}
	}
	return nil
}

func persistMailLogRecord(db *sql.DB, stateKey string, record lineRecord, entry *LogEntry) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin maillog record transaction: %w", err)
	}
	defer tx.Rollback()

	if entry != nil {
		_, err := insertRawEntry(tx, entry)
		if err != nil {
			return fmt.Errorf("insert raw maillog entry: %w", err)
		}
		if entry.QueueID != "" {
			if err := mergeMailTransaction(tx, entry); err != nil {
				return fmt.Errorf("merge mail transaction: %w", err)
			}
		}
	}
	if err := setIngestStateTx(tx, stateKey, record.Offset, record.Inode); err != nil {
		return fmt.Errorf("save ingest checkpoint: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit maillog record transaction: %w", err)
	}
	return nil
}

func continuousIngestStateKey(path string) string {
	return "continuous:" + path
}

func insertRawEntry(tx *sql.Tx, entry *LogEntry) (bool, error) {
	result, err := tx.Exec(`
INSERT INTO maillog_entries (
	ts_utc, host, process, queue_id, mail_from, mail_to, status, relay, delay, delays, dsn, message_id, size_bytes, queued_as, mail_id, subject, hits, helo, amavis_origin, is_junk, spam_score, raw, raw_hash
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23)
ON CONFLICT(raw_hash) DO NOTHING;
`,
		entry.TSUTC.UTC(), entry.Host, entry.Process, entry.QueueID, entry.MailFrom, entry.MailTo,
		entry.Status, entry.Relay, nullFloat(entry.Delay), entry.Delays, entry.DSN, entry.MessageID,
		nullInt64(entry.SizeBytes), entry.QueuedAs, entry.MailID, entry.Subject, nullFloat(entry.Hits),
		entry.Helo, entry.AmavisOrigin, entry.IsJunk, nullFloat(entry.SpamScore), entry.Raw, entry.RawHash,
	)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows > 0, err
}

func mergeMailTransaction(tx *sql.Tx, entry *LogEntry) error {
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1));`, entry.QueueID); err != nil {
		return err
	}
	terminal := isTerminalStatus(entry.Status)
	result, err := tx.Exec(`
UPDATE mail_transactions
SET last_ts_utc = $2,
	host = CASE WHEN $3 <> '' THEN $3 ELSE host END,
	process = CASE WHEN $4 <> '' THEN $4 ELSE process END,
	mail_from = CASE WHEN $5 <> '' THEN $5 ELSE mail_from END,
	mail_to = CASE WHEN $6 <> '' THEN $6 ELSE mail_to END,
	status = CASE WHEN $7 <> '' THEN $7 ELSE status END,
	relay = CASE WHEN $8 <> '' THEN $8 ELSE relay END,
	delay = COALESCE($9, delay),
	delays = CASE WHEN $10 <> '' THEN $10 ELSE delays END,
	dsn = CASE WHEN $11 <> '' THEN $11 ELSE dsn END,
	message_id = CASE WHEN $12 <> '' THEN $12 ELSE message_id END,
	size_bytes = COALESCE($13, size_bytes),
	queued_as = CASE WHEN $14 <> '' THEN $14 ELSE queued_as END,
	mail_id = CASE WHEN $15 <> '' THEN $15 ELSE mail_id END,
	subject = CASE WHEN $16 <> '' THEN $16 ELSE subject END,
	hits = COALESCE($17, hits),
	helo = CASE WHEN $18 <> '' THEN $18 ELSE helo END,
	amavis_origin = CASE WHEN $19 <> '' THEN $19 ELSE amavis_origin END,
	is_junk = is_junk OR $20,
	spam_score = COALESCE($21, spam_score),
	raw = CASE WHEN raw = '' THEN $22 ELSE raw || E'\n' || $22 END,
	terminal = terminal OR $23,
	updated_at = NOW()
WHERE id = (
	SELECT id FROM mail_transactions
	WHERE queue_id = $1 AND terminal = FALSE
	ORDER BY id DESC
	LIMIT 1
	FOR UPDATE
);
`,
		entry.QueueID, entry.TSUTC.UTC(), entry.Host, entry.Process, entry.MailFrom, entry.MailTo,
		entry.Status, entry.Relay, nullFloat(entry.Delay), entry.Delays, entry.DSN, entry.MessageID,
		nullInt64(entry.SizeBytes), entry.QueuedAs, entry.MailID, entry.Subject, nullFloat(entry.Hits),
		entry.Helo, entry.AmavisOrigin, entry.IsJunk, nullFloat(entry.SpamScore), entry.Raw, terminal,
	)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil || rows > 0 {
		return err
	}
	_, err = tx.Exec(`
INSERT INTO mail_transactions (
	arrival_ts_utc, last_ts_utc, queue_id, host, process, mail_from, mail_to, status, relay,
	delay, delays, dsn, message_id, size_bytes, queued_as, mail_id, subject, hits, helo,
	amavis_origin, is_junk, spam_score, raw, terminal, timed_out, updated_at
) VALUES (
	$1, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17,
	$18, $19, $20, $21, $22, $23, FALSE, NOW()
);
`,
		entry.TSUTC.UTC(), entry.QueueID, entry.Host, entry.Process, entry.MailFrom, entry.MailTo,
		entry.Status, entry.Relay, nullFloat(entry.Delay), entry.Delays, entry.DSN, entry.MessageID,
		nullInt64(entry.SizeBytes), entry.QueuedAs, entry.MailID, entry.Subject, nullFloat(entry.Hits),
		entry.Helo, entry.AmavisOrigin, entry.IsJunk, nullFloat(entry.SpamScore), entry.Raw, terminal,
	)
	return err
}

func evictExpiredTransactions(db *sql.DB, cutoff time.Time) error {
	_, err := db.Exec(`
UPDATE mail_transactions
SET terminal = TRUE,
	timed_out = TRUE,
	status = CASE WHEN status = '' THEN 'timed_out' ELSE status END,
	updated_at = NOW()
WHERE terminal = FALSE AND updated_at < $1;
`, cutoff.UTC())
	if err != nil {
		return fmt.Errorf("evict idle mail transactions: %w", err)
	}
	return nil
}

func setIngestStateTx(tx *sql.Tx, key string, offset int64, inode int64) error {
	_, err := tx.Exec(`
INSERT INTO ingest_state (key, offset_bytes, inode, updated_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT(key) DO UPDATE SET offset_bytes=excluded.offset_bytes, inode=excluded.inode, updated_at=excluded.updated_at;
`, key, offset, inode, time.Now().UTC())
	return err
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
