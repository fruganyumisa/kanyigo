package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
)

type StreamConfig struct {
	Path                 string
	PollInterval         time.Duration
	RotationDrainTimeout time.Duration
	QueueIdleTimeout     time.Duration
	ProcessingWorkers    int
}

type lineRecord struct {
	Sequence int64
	Text     string
	Inode    int64
	Offset   int64
}

type MailTransaction struct {
	ArrivalTimestamp time.Time `json:"arrival_timestamp"`
	LastTimestamp    time.Time `json:"last_timestamp"`
	QueueID          string    `json:"queue_id"`
	Host             string    `json:"host"`
	Process          string    `json:"process"`
	Sender           string    `json:"sender"`
	Recipient        string    `json:"recipient"`
	Status           string    `json:"status"`
	Relay            string    `json:"relay"`
	Delay            *float64  `json:"delay,omitempty"`
	Delays           string    `json:"delays"`
	DSN              string    `json:"dsn"`
	MessageID        string    `json:"message_id"`
	SizeBytes        *int64    `json:"size_bytes,omitempty"`
	QueuedAs         string    `json:"queued_as"`
	MailID           string    `json:"mail_id"`
	Subject          string    `json:"subject"`
	Hits             *float64  `json:"hits,omitempty"`
	Helo             string    `json:"helo"`
	AmavisOrigin     string    `json:"amavis_origin"`
	IsJunk           bool      `json:"is_junk"`
	SpamScore        float64   `json:"spam_score"`
	TimedOut         bool      `json:"timed_out"`
	Raw              []string  `json:"raw"`
}

type queuedTransaction struct {
	transaction MailTransaction
	lastSeen    time.Time
}

type TransactionStitcher struct {
	mu          sync.Mutex
	queues      map[string]*queuedTransaction
	idleTimeout time.Duration
}

func NewTransactionStitcher(idleTimeout time.Duration) *TransactionStitcher {
	return &TransactionStitcher{
		queues:      make(map[string]*queuedTransaction),
		idleTimeout: idleTimeout,
	}
}

func (s *TransactionStitcher) Apply(entry *LogEntry) (*MailTransaction, bool) {
	if entry == nil || entry.QueueID == "" {
		return nil, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	queue, ok := s.queues[entry.QueueID]
	if !ok {
		queue = &queuedTransaction{
			transaction: MailTransaction{
				ArrivalTimestamp: entry.TSUTC,
				QueueID:          entry.QueueID,
			},
		}
		s.queues[entry.QueueID] = queue
	}
	tx := &queue.transaction
	tx.LastTimestamp = entry.TSUTC
	queue.lastSeen = time.Now()
	if entry.Host != "" {
		tx.Host = entry.Host
	}
	if entry.Process != "" {
		tx.Process = entry.Process
	}
	if entry.MailFrom != "" {
		tx.Sender = entry.MailFrom
	}
	if entry.MailTo != "" {
		tx.Recipient = entry.MailTo
	}
	if entry.Status != "" {
		tx.Status = entry.Status
	}
	if entry.Relay != "" {
		tx.Relay = entry.Relay
	}
	if entry.Delay != nil {
		delay := *entry.Delay
		tx.Delay = &delay
	}
	if entry.Delays != "" {
		tx.Delays = entry.Delays
	}
	if entry.DSN != "" {
		tx.DSN = entry.DSN
	}
	if entry.MessageID != "" {
		tx.MessageID = entry.MessageID
	}
	if entry.SizeBytes != nil {
		size := *entry.SizeBytes
		tx.SizeBytes = &size
	}
	if entry.QueuedAs != "" {
		tx.QueuedAs = entry.QueuedAs
	}
	if entry.MailID != "" {
		tx.MailID = entry.MailID
	}
	if entry.Subject != "" {
		tx.Subject = entry.Subject
	}
	if entry.Hits != nil {
		hits := *entry.Hits
		tx.Hits = &hits
	}
	if entry.Helo != "" {
		tx.Helo = entry.Helo
	}
	if entry.AmavisOrigin != "" {
		tx.AmavisOrigin = entry.AmavisOrigin
	}
	if entry.IsJunk {
		tx.IsJunk = true
	}
	if entry.SpamScore != nil {
		tx.SpamScore = *entry.SpamScore
	}
	tx.Raw = append(tx.Raw, entry.Raw)
	if !isTerminalStatus(tx.Status) {
		return nil, false
	}

	result := *tx
	delete(s.queues, entry.QueueID)
	return &result, true
}

func (s *TransactionStitcher) EvictExpired(now time.Time) []MailTransaction {
	s.mu.Lock()
	defer s.mu.Unlock()

	var expired []MailTransaction
	for queueID, queue := range s.queues {
		if now.Sub(queue.lastSeen) < s.idleTimeout {
			continue
		}
		tx := queue.transaction
		tx.TimedOut = true
		expired = append(expired, tx)
		delete(s.queues, queueID)
	}
	return expired
}

func isTerminalStatus(status string) bool {
	switch strings.ToLower(status) {
	case "sent", "bounced":
		return true
	default:
		return false
	}
}

func validateStreamConfig(cfg StreamConfig) error {
	if cfg.Path == "" {
		return errors.New("maillog path is required")
	}
	if cfg.PollInterval <= 0 || cfg.RotationDrainTimeout <= 0 || cfg.QueueIdleTimeout <= 0 {
		return errors.New("poll interval, rotation drain timeout, and queue idle timeout must be positive")
	}
	if cfg.ProcessingWorkers <= 0 {
		return errors.New("processing workers must be positive")
	}
	return nil
}

func followMailLog(ctx context.Context, cfg StreamConfig, checkpoint ingestState, output chan<- lineRecord) error {
	if err := validateStreamConfig(cfg); err != nil {
		return err
	}
	file, reader, inode, offset, err := openMailLog(cfg.Path, checkpoint)
	if err != nil {
		return err
	}
	defer file.Close()

	var pending strings.Builder
	var rotatedAt time.Time
	var sequence int64
	for {
		chunk, readErr := reader.ReadString('\n')
		offset += int64(len(chunk))
		pending.WriteString(chunk)
		if strings.HasSuffix(chunk, "\n") {
			record := lineRecord{
				Sequence: sequence,
				Text:     strings.TrimSuffix(pending.String(), "\n"),
				Inode:    inode,
				Offset:   offset,
			}
			sequence++
			pending.Reset()
			select {
			case output <- record:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return fmt.Errorf("read %s: %w", cfg.Path, readErr)
		}
		if readErr == nil {
			continue
		}

		currentInfo, statErr := file.Stat()
		if statErr != nil {
			return fmt.Errorf("stat open maillog: %w", statErr)
		}
		pathInfo, pathErr := os.Stat(cfg.Path)
		switch {
		case pathErr != nil && !os.IsNotExist(pathErr):
			return fmt.Errorf("stat maillog path: %w", pathErr)
		case pathErr == nil && fileInode(pathInfo) == inode && currentInfo.Size() < offset:
			file, reader, inode, offset, err = reopenMailLog(file, cfg.Path)
			if err != nil {
				return err
			}
			pending.Reset()
			rotatedAt = time.Time{}
			continue
		case pathErr == nil && fileInode(pathInfo) != inode:
			if rotatedAt.IsZero() {
				rotatedAt = time.Now()
			}
			if time.Since(rotatedAt) >= cfg.RotationDrainTimeout {
				file, reader, inode, offset, err = reopenMailLog(file, cfg.Path)
				if err != nil {
					return err
				}
				pending.Reset()
				rotatedAt = time.Time{}
				continue
			}
		default:
			rotatedAt = time.Time{}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(cfg.PollInterval):
		}
	}
}

func openMailLog(path string, checkpoint ingestState) (*os.File, *bufio.Reader, int64, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, 0, 0, fmt.Errorf("open maillog %s: %w", path, err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, 0, 0, fmt.Errorf("stat maillog %s: %w", path, err)
	}
	inode := fileInode(info)
	offset := int64(0)
	if checkpoint.Inode == inode && checkpoint.OffsetBytes >= 0 && checkpoint.OffsetBytes <= info.Size() {
		offset = checkpoint.OffsetBytes
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		_ = file.Close()
		return nil, nil, 0, 0, fmt.Errorf("seek maillog %s: %w", path, err)
	}
	return file, bufio.NewReader(file), inode, offset, nil
}

func reopenMailLog(oldFile *os.File, path string) (*os.File, *bufio.Reader, int64, int64, error) {
	file, reader, inode, offset, err := openMailLog(path, ingestState{})
	if err != nil {
		return oldFile, nil, 0, 0, err
	}
	if err := oldFile.Close(); err != nil {
		_ = file.Close()
		return oldFile, nil, 0, 0, fmt.Errorf("close rotated maillog: %w", err)
	}
	return file, reader, inode, offset, nil
}

func fileInode(info os.FileInfo) int64 {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return int64(stat.Ino)
	}
	return 0
}
