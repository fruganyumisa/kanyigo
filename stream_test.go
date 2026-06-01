package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTransactionStitcherCorrelatesQueueLines(t *testing.T) {
	stitcher := NewTransactionStitcher(time.Hour)
	arrival := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	delay := 1.25

	if tx, complete := stitcher.Apply(&LogEntry{
		TSUTC:    arrival,
		QueueID:  "ABC123",
		MailFrom: "sender@example.com",
	}); complete || tx != nil {
		t.Fatal("sender line must not complete transaction")
	}
	if tx, complete := stitcher.Apply(&LogEntry{
		TSUTC:   arrival.Add(time.Second),
		QueueID: "ABC123",
		MailTo:  "receiver@example.com",
		Status:  "deferred",
	}); complete || tx != nil {
		t.Fatal("deferred line must not complete transaction")
	}
	tx, complete := stitcher.Apply(&LogEntry{
		TSUTC:   arrival.Add(2 * time.Second),
		QueueID: "ABC123",
		Status:  "sent",
		Delay:   &delay,
		Delays:  "0.1/0.2/0.3/0.65",
	})
	if !complete {
		t.Fatal("sent line must complete transaction")
	}
	if tx.Sender != "sender@example.com" || tx.Recipient != "receiver@example.com" {
		t.Fatalf("unexpected stitched transaction: %+v", tx)
	}
	if tx.Delay == nil || *tx.Delay != delay || tx.Delays != "0.1/0.2/0.3/0.65" {
		t.Fatalf("unexpected delay metrics: %+v", tx)
	}
}

func TestTransactionStitcherEvictsIdleQueue(t *testing.T) {
	stitcher := NewTransactionStitcher(time.Millisecond)
	stitcher.Apply(&LogEntry{
		TSUTC:   time.Now().UTC(),
		QueueID: "ABC123",
		Status:  "deferred",
	})
	expired := stitcher.EvictExpired(time.Now().Add(time.Second))
	if len(expired) != 1 || !expired[0].TimedOut {
		t.Fatalf("unexpected expired transactions: %+v", expired)
	}
}

func TestTransactionStitcherCarriesJunkClassification(t *testing.T) {
	stitcher := NewTransactionStitcher(time.Hour)
	score := 8.75
	stitcher.Apply(&LogEntry{
		TSUTC:     time.Now().UTC(),
		QueueID:   "ABC123",
		IsJunk:    true,
		SpamScore: &score,
	})
	tx, complete := stitcher.Apply(&LogEntry{
		TSUTC:   time.Now().UTC(),
		QueueID: "ABC123",
		Status:  "sent",
	})
	if !complete {
		t.Fatal("sent line must complete transaction")
	}
	if !tx.IsJunk || tx.SpamScore != score {
		t.Fatalf("unexpected junk fields: %+v", tx)
	}
}

func TestFollowMailLogResumesFromCheckpoint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "maillog")
	first := "first line\n"
	second := "second line\n"
	if err := os.WriteFile(path, []byte(first+second), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := ingestState{
		Inode:       fileInode(info),
		OffsetBytes: int64(len(first)),
	}

	records, cancel := startFollower(t, path, checkpoint)
	defer cancel()
	record := waitRecord(t, records)
	if record.Text != "second line" || record.Offset != int64(len(first+second)) {
		t.Fatalf("unexpected resumed record: %+v", record)
	}
}

func TestFollowMailLogDrainsRotationAndReadsReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "maillog")
	rotatedPath := filepath.Join(dir, "maillog.1")
	if err := os.WriteFile(path, []byte("initial line\n"), 0600); err != nil {
		t.Fatal(err)
	}
	records, cancel := startFollower(t, path, ingestState{})
	defer cancel()
	_ = waitRecord(t, records)

	if err := os.Rename(path, rotatedPath); err != nil {
		t.Fatal(err)
	}
	if err := appendFile(rotatedPath, "rotated descriptor line\n"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement line\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if record := waitRecord(t, records); record.Text != "rotated descriptor line" {
		t.Fatalf("expected rotated descriptor line, got %+v", record)
	}
	if record := waitRecord(t, records); record.Text != "replacement line" {
		t.Fatalf("expected replacement line, got %+v", record)
	}
}

func TestFollowMailLogReopensTruncatedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "maillog")
	if err := os.WriteFile(path, []byte("a deliberately long initial line\n"), 0600); err != nil {
		t.Fatal(err)
	}
	records, cancel := startFollower(t, path, ingestState{})
	defer cancel()
	_ = waitRecord(t, records)

	if err := os.WriteFile(path, []byte("short line\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if record := waitRecord(t, records); record.Text != "short line" {
		t.Fatalf("expected line after truncation, got %+v", record)
	}
}

func TestParseLineLegacyNewYearRollover(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 30, 0, time.UTC)
	entry, ok := ParseLine("Dec 31 23:59:59 mail postfix/qmgr[1]: ABC123: from=<a@example.com>", now, time.UTC)
	if !ok {
		t.Fatal("expected legacy line to parse")
	}
	want := time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC)
	if !entry.TSUTC.Equal(want) {
		t.Fatalf("timestamp: got %s want %s", entry.TSUTC, want)
	}
}

func TestParseLineLegacyNewYearRolloverIntoJanuary(t *testing.T) {
	now := time.Date(2025, 12, 31, 23, 59, 30, 0, time.UTC)
	entry, ok := ParseLine("Jan  1 00:00:01 mail postfix/qmgr[1]: ABC123: from=<a@example.com>", now, time.UTC)
	if !ok {
		t.Fatal("expected legacy line to parse")
	}
	want := time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC)
	if !entry.TSUTC.Equal(want) {
		t.Fatalf("timestamp: got %s want %s", entry.TSUTC, want)
	}
}

func startFollower(t *testing.T, path string, checkpoint ingestState) (<-chan lineRecord, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	records := make(chan lineRecord, 16)
	cfg := StreamConfig{
		Path:                 path,
		PollInterval:         10 * time.Millisecond,
		RotationDrainTimeout: 40 * time.Millisecond,
		QueueIdleTimeout:     time.Hour,
		ProcessingWorkers:    1,
	}
	go func() {
		if err := followMailLog(ctx, cfg, checkpoint, records); err != nil && err != context.Canceled {
			t.Errorf("followMailLog: %v", err)
		}
	}()
	return records, cancel
}

func waitRecord(t *testing.T, records <-chan lineRecord) lineRecord {
	t.Helper()
	select {
	case record := <-records:
		return record
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for maillog record")
		return lineRecord{}
	}
}

func appendFile(path, content string) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.WriteString(content)
	return err
}
