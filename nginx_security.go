package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type NginxSecurityConfig struct {
	Path                    string
	PollInterval            time.Duration
	RotationDrainTimeout    time.Duration
	Consecutive404Threshold int
	Consecutive404Window    time.Duration
	AuthFailureThreshold    int
	AuthFailureWindow       time.Duration
	IgnoredPaths            map[string]bool
	TrustedProxyCIDRs       []*net.IPNet
}

type NginxEvent struct {
	Timestamp time.Time
	RemoteIP  string
	Method    string
	Path      string
	Status    int
	UserAgent string
	Raw       string
	RawHash   string
}

var nginxCombinedLine = regexp.MustCompile(`^(\S+)\s+\S+\s+\S+\s+\[([^]]+)\]\s+"(\S+)\s+([^\s"]+)(?:\s+[^\"]+)?"\s+(\d{3})\s+\S+\s+"[^"]*"\s+"([^"]*)"`)

func ParseNginxLine(line string, cfg NginxSecurityConfig) (*NginxEvent, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, false
	}
	if event, ok := parseNginxJSON(line, cfg); ok {
		return event, true
	}
	match := nginxCombinedLine.FindStringSubmatch(line)
	if match == nil {
		return nil, false
	}
	timestamp, err := time.Parse("02/Jan/2006:15:04:05 -0700", match[2])
	if err != nil {
		return nil, false
	}
	status, err := strconv.Atoi(match[5])
	if err != nil {
		return nil, false
	}
	return newNginxEvent(line, timestamp, match[1], "", match[3], match[4], status, match[6], cfg)
}

func parseNginxJSON(line string, cfg NginxSecurityConfig) (*NginxEvent, bool) {
	var values map[string]interface{}
	if err := json.Unmarshal([]byte(line), &values); err != nil {
		return nil, false
	}
	remoteIP := jsonString(values, "remote_addr")
	forwardedFor := jsonString(values, "http_x_forwarded_for")
	request := jsonString(values, "request")
	method := jsonString(values, "request_method")
	path := jsonString(values, "uri")
	if request != "" {
		parts := strings.Fields(request)
		if len(parts) >= 2 {
			if method == "" {
				method = parts[0]
			}
			if path == "" {
				path = parts[1]
			}
		}
	}
	status, err := jsonInt(values, "status")
	if err != nil {
		return nil, false
	}
	timestamp, err := nginxJSONTimestamp(values)
	if err != nil {
		return nil, false
	}
	return newNginxEvent(line, timestamp, remoteIP, forwardedFor, method, path, status, jsonString(values, "http_user_agent"), cfg)
}

func newNginxEvent(line string, timestamp time.Time, remoteIP, forwardedFor, method, path string, status int, userAgent string, cfg NginxSecurityConfig) (*NginxEvent, bool) {
	ip := net.ParseIP(strings.TrimSpace(remoteIP))
	if ip == nil {
		return nil, false
	}
	if isTrustedProxy(ip, cfg.TrustedProxyCIDRs) {
		if forwarded := forwardedClientIP(forwardedFor, cfg.TrustedProxyCIDRs); forwarded != nil {
			ip = forwarded
		}
	}
	if parsed, err := url.ParseRequestURI(path); err == nil && parsed.Path != "" {
		path = parsed.Path
	}
	hash := sha256.Sum256([]byte(line))
	return &NginxEvent{
		Timestamp: timestamp.UTC(),
		RemoteIP:  ip.String(),
		Method:    method,
		Path:      path,
		Status:    status,
		UserAgent: userAgent,
		Raw:       line,
		RawHash:   hex.EncodeToString(hash[:]),
	}, true
}

func nginxJSONTimestamp(values map[string]interface{}) (time.Time, error) {
	for _, key := range []string{"time_iso8601", "timestamp"} {
		if value := jsonString(values, key); value != "" {
			if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
				return parsed, nil
			}
		}
	}
	if value := jsonString(values, "time_local"); value != "" {
		return time.Parse("02/Jan/2006:15:04:05 -0700", value)
	}
	if value, ok := values["msec"]; ok {
		seconds, err := strconv.ParseFloat(fmt.Sprint(value), 64)
		if err == nil {
			whole := int64(seconds)
			return time.Unix(whole, int64((seconds-float64(whole))*1e9)), nil
		}
	}
	return time.Time{}, errors.New("missing nginx timestamp")
}

func jsonString(values map[string]interface{}, key string) string {
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func jsonInt(values map[string]interface{}, key string) (int, error) {
	value, ok := values[key]
	if !ok {
		return 0, errors.New("missing integer")
	}
	switch typed := value.(type) {
	case float64:
		return int(typed), nil
	case string:
		return strconv.Atoi(typed)
	default:
		return strconv.Atoi(fmt.Sprint(value))
	}
}

func forwardedClientIP(value string, trusted []*net.IPNet) net.IP {
	parts := strings.Split(value, ",")
	for index := len(parts) - 1; index >= 0; index-- {
		if ip := net.ParseIP(strings.TrimSpace(parts[index])); ip != nil && !isTrustedProxy(ip, trusted) {
			return ip
		}
	}
	return nil
}

func isTrustedProxy(ip net.IP, networks []*net.IPNet) bool {
	for _, network := range networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func RunNginxSecurityIngestor(ctx context.Context, db *sql.DB, cfg NginxSecurityConfig) error {
	streamCfg := StreamConfig{
		Path:                 cfg.Path,
		PollInterval:         cfg.PollInterval,
		RotationDrainTimeout: cfg.RotationDrainTimeout,
		QueueIdleTimeout:     time.Hour,
		ProcessingWorkers:    1,
	}
	stateKey := "nginx-security:" + cfg.Path
	state, err := getIngestState(db, stateKey)
	if err != nil {
		return fmt.Errorf("load nginx checkpoint: %w", err)
	}
	records := make(chan lineRecord, 1024)
	errorsCh := make(chan error, 1)
	go func() { errorsCh <- followMailLog(ctx, streamCfg, state, records) }()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-errorsCh:
			if err == nil || errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		case record := <-records:
			event, ok := ParseNginxLine(record.Text, cfg)
			if !ok {
				log.Printf("skip malformed nginx line at inode=%d offset=%d", record.Inode, record.Offset)
			}
			if err := persistNginxEvent(db, stateKey, record, event, cfg); err != nil {
				return err
			}
		}
	}
}

func persistNginxEvent(db *sql.DB, stateKey string, record lineRecord, event *NginxEvent, cfg NginxSecurityConfig) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if event != nil {
		suspicious := event.Status == 401 || event.Status == 403 || event.Status == httpStatusNotFound
		if suspicious {
			if _, err := tx.Exec(`
INSERT INTO nginx_security_events (ts_utc, remote_ip, method, path, status, user_agent, raw, raw_hash)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);
			`, event.Timestamp, event.RemoteIP, event.Method, event.Path, event.Status, event.UserAgent, event.Raw, event.RawHash); err != nil {
				return fmt.Errorf("insert nginx event: %w", err)
			}
		}
		if event.Status != httpStatusNotFound || !cfg.IgnoredPaths[event.Path] {
			if err := updateNginxDetection(tx, event, cfg); err != nil {
				return err
			}
		}
	}
	if err := setIngestStateTx(tx, stateKey, record.Offset, record.Inode); err != nil {
		return fmt.Errorf("save nginx checkpoint: %w", err)
	}
	return tx.Commit()
}

func updateNginxDetection(tx *sql.Tx, event *NginxEvent, cfg NginxSecurityConfig) error {
	if event.Status == httpStatusNotFound {
		var consecutive int
		err := tx.QueryRow(`
INSERT INTO nginx_ip_detection_state (remote_ip, consecutive_404, streak_started_at, last_seen_at, last_status)
VALUES ($1, 1, $2, $2, $3)
ON CONFLICT(remote_ip) DO UPDATE SET
	consecutive_404 = CASE
		WHEN nginx_ip_detection_state.last_status = 404
		 AND EXCLUDED.last_seen_at >= nginx_ip_detection_state.last_seen_at
			 AND EXCLUDED.last_seen_at <= nginx_ip_detection_state.streak_started_at + $4::interval
		THEN nginx_ip_detection_state.consecutive_404 + 1 ELSE 1 END,
	streak_started_at = CASE
		WHEN nginx_ip_detection_state.last_status = 404
		 AND EXCLUDED.last_seen_at >= nginx_ip_detection_state.last_seen_at
			 AND EXCLUDED.last_seen_at <= nginx_ip_detection_state.streak_started_at + $4::interval
		THEN nginx_ip_detection_state.streak_started_at ELSE EXCLUDED.last_seen_at END,
	last_seen_at = EXCLUDED.last_seen_at,
	last_status = EXCLUDED.last_status
RETURNING consecutive_404;
`, event.RemoteIP, event.Timestamp, event.Status, fmt.Sprintf("%f seconds", cfg.Consecutive404Window.Seconds())).Scan(&consecutive)
		if err != nil {
			return fmt.Errorf("update 404 streak: %w", err)
		}
		if consecutive >= cfg.Consecutive404Threshold {
			return flagSecurityOffender(tx, event, "consecutive_404", consecutive)
		}
		return nil
	}

	if _, err := tx.Exec(`
UPDATE nginx_ip_detection_state
SET consecutive_404=0, streak_started_at=NULL, last_seen_at=$2, last_status=$3
WHERE remote_ip=$1;
`, event.RemoteIP, event.Timestamp, event.Status); err != nil {
		return fmt.Errorf("reset 404 streak: %w", err)
	}

	if event.Status == 401 || event.Status == 403 {
		var attempts int
		if err := tx.QueryRow(`
SELECT COUNT(*) FROM nginx_security_events
WHERE remote_ip=$1 AND status IN (401, 403) AND ts_utc BETWEEN $2 AND $3;
`, event.RemoteIP, event.Timestamp.Add(-cfg.AuthFailureWindow), event.Timestamp).Scan(&attempts); err != nil {
			return fmt.Errorf("count auth failures: %w", err)
		}
		if attempts >= cfg.AuthFailureThreshold {
			return flagSecurityOffender(tx, event, "authentication_failures", attempts)
		}
	}
	return nil
}

const httpStatusNotFound = 404

func flagSecurityOffender(tx *sql.Tx, event *NginxEvent, reason string, attempts int) error {
	_, err := tx.Exec(`
INSERT INTO security_offenders (remote_ip, reason, first_seen_at, last_seen_at, attempt_count, last_path, flagged, dismissed_at, updated_at)
VALUES ($1, $2, $3, $3, $4, $5, TRUE, NULL, NOW())
ON CONFLICT(remote_ip) DO UPDATE SET
	reason=EXCLUDED.reason,
	last_seen_at=EXCLUDED.last_seen_at,
	attempt_count=EXCLUDED.attempt_count,
	last_path=EXCLUDED.last_path,
	flagged=TRUE,
	dismissed_at=NULL,
	updated_at=NOW();
`, event.RemoteIP, reason, event.Timestamp, attempts, event.Path)
	return err
}

func parseCIDRs(value string) ([]*net.IPNet, error) {
	var networks []*net.IPNet
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		_, network, err := net.ParseCIDR(item)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q: %w", item, err)
		}
		networks = append(networks, network)
	}
	return networks, nil
}
