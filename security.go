package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

type securityOffender struct {
	IP           string  `json:"ip"`
	Reason       string  `json:"reason"`
	FirstSeenAt  string  `json:"firstSeenAt"`
	LastSeenAt   string  `json:"lastSeenAt"`
	AttemptCount int     `json:"attemptCount"`
	LastPath     string  `json:"lastPath"`
	Flagged      bool    `json:"flagged"`
	Blocked      bool    `json:"blocked"`
	BlockedAt    *string `json:"blockedAt"`
	ExpiresAt    *string `json:"expiresAt"`
	LastError    string  `json:"lastError"`
}

func (s *Server) handleSecurity(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireAdmin(w, r); !ok {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	_, _ = s.db.Exec(`UPDATE firewall_blocks SET active=FALSE, updated_at=NOW() WHERE active=TRUE AND expires_at IS NOT NULL AND expires_at <= NOW()`)

	limit := parseIntDefault(r.URL.Query().Get("limit"), 100)
	if limit > 500 {
		limit = 500
	}
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	where := "WHERE (o.flagged=TRUE OR COALESCE(b.active, FALSE)=TRUE) AND ($1 = '' OR o.remote_ip ILIKE '%' || $1 || '%' OR o.last_path ILIKE '%' || $1 || '%')"
	rows, err := s.db.Query(`
SELECT o.remote_ip, o.reason, o.first_seen_at, o.last_seen_at, o.attempt_count, o.last_path, o.flagged,
	COALESCE(b.active, FALSE), b.blocked_at, b.expires_at, COALESCE(b.last_error, '')
FROM security_offenders o
LEFT JOIN firewall_blocks b ON b.remote_ip=o.remote_ip
`+where+`
ORDER BY COALESCE(b.active, FALSE) DESC, o.last_seen_at DESC
LIMIT $2;
`, search, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	items := []securityOffender{}
	for rows.Next() {
		var item securityOffender
		var firstSeen, lastSeen time.Time
		var blockedAt, expiresAt sql.NullTime
		if err := rows.Scan(&item.IP, &item.Reason, &firstSeen, &lastSeen, &item.AttemptCount, &item.LastPath,
			&item.Flagged, &item.Blocked, &blockedAt, &expiresAt, &item.LastError); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		item.FirstSeenAt = firstSeen.UTC().Format(time.RFC3339)
		item.LastSeenAt = lastSeen.UTC().Format(time.RFC3339)
		if blockedAt.Valid {
			value := blockedAt.Time.UTC().Format(time.RFC3339)
			item.BlockedAt = &value
		}
		if expiresAt.Valid {
			value := expiresAt.Time.UTC().Format(time.RFC3339)
			item.ExpiresAt = &value
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	var flagged, blocked, attemptsToday int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM security_offenders WHERE flagged=TRUE`).Scan(&flagged)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM firewall_blocks WHERE active=TRUE`).Scan(&blocked)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM nginx_security_events WHERE ts_utc >= date_trunc('day', NOW()) AND status IN (401,403,404)`).Scan(&attemptsToday)
	firewallHealthy := s.firewall != nil && s.firewall.Status() == nil
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": items,
		"summary": map[string]interface{}{
			"flagged": flagged, "blocked": blocked, "attemptsToday": attemptsToday, "firewallHealthy": firewallHealthy,
		},
	})
}

func (s *Server) handleSecurityBlock(w http.ResponseWriter, r *http.Request) {
	user, ok := requireAdmin(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	var request struct {
		IP              string `json:"ip"`
		DurationSeconds int64  `json:"durationSeconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid JSON body"))
		return
	}
	ip, err := s.validateBlockTarget(request.IP, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if request.DurationSeconds < 0 || request.DurationSeconds > int64((365*24*time.Hour)/time.Second) {
		writeError(w, http.StatusBadRequest, errors.New("block duration must be between 0 and 365 days"))
		return
	}
	action := "block"
	operation := func() error { return s.firewall.Block(ip, request.DurationSeconds) }
	if r.Method == http.MethodDelete {
		action = "unblock"
		operation = func() error { return s.firewall.Unblock(ip) }
	}
	if s.firewall == nil || !s.firewall.Available() {
		err = errors.New("firewall agent is not configured")
	} else {
		err = operation()
	}
	if persistErr := s.recordFirewallResult(user.ID, action, ip, request.DurationSeconds, err); persistErr != nil {
		writeError(w, http.StatusInternalServerError, persistErr)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": action + "ed", "ip": ip})
}

func (s *Server) handleSecurityDismiss(w http.ResponseWriter, r *http.Request) {
	user, ok := requireAdmin(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	var request struct {
		IP string `json:"ip"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid JSON body"))
		return
	}
	ip := net.ParseIP(strings.TrimSpace(request.IP))
	if ip == nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid IP address"))
		return
	}
	result, err := s.db.Exec(`UPDATE security_offenders SET flagged=FALSE, dismissed_at=NOW(), updated_at=NOW() WHERE remote_ip=$1`, ip.String())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		writeError(w, http.StatusNotFound, errors.New("offender not found"))
		return
	}
	_, _ = s.db.Exec(`INSERT INTO security_audit_log (ts_utc, actor_user_id, action, remote_ip, detail, success) VALUES (NOW(), $1, 'dismiss', $2, '', TRUE)`, user.ID, ip.String())
	writeJSON(w, http.StatusOK, map[string]string{"status": "dismissed"})
}

func requireAdmin(w http.ResponseWriter, r *http.Request) (authUser, bool) {
	user, ok := currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, errors.New("authentication required"))
		return authUser{}, false
	}
	if user.Role != "admin" {
		writeError(w, http.StatusForbidden, errors.New("admin access required"))
		return authUser{}, false
	}
	return user, true
}

func (s *Server) validateBlockTarget(value string, r *http.Request) (string, error) {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil {
		return "", errors.New("invalid IP address")
	}
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() {
		return "", errors.New("refusing to modify a protected IP address")
	}
	for _, network := range s.securityAllowlist {
		if network.Contains(ip) {
			return "", errors.New("IP address is in SECURITY_IP_ALLOWLIST")
		}
	}
	if requestIP := net.ParseIP(clientIP(r)); requestIP != nil && requestIP.Equal(ip) {
		return "", errors.New("refusing to block the current administrator IP")
	}
	return ip.String(), nil
}

func (s *Server) recordFirewallResult(userID int64, action, ip string, durationSeconds int64, operationErr error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	success := operationErr == nil
	detail := fmt.Sprintf("duration_seconds=%d", durationSeconds)
	if operationErr != nil {
		detail = operationErr.Error()
	}
	if action == "block" {
		var expires interface{}
		if durationSeconds > 0 {
			expires = time.Now().UTC().Add(time.Duration(durationSeconds) * time.Second)
		}
		_, err = tx.Exec(`
INSERT INTO firewall_blocks (remote_ip, active, blocked_at, expires_at, blocked_by, last_error, updated_at)
VALUES ($1, $2, CASE WHEN $2 THEN NOW() ELSE NULL END, $3, $4, $5, NOW())
ON CONFLICT(remote_ip) DO UPDATE SET active=EXCLUDED.active, blocked_at=EXCLUDED.blocked_at, expires_at=EXCLUDED.expires_at,
	blocked_by=EXCLUDED.blocked_by, last_error=EXCLUDED.last_error, updated_at=NOW();
`, ip, success, expires, userID, errorString(operationErr))
	} else {
		_, err = tx.Exec(`UPDATE firewall_blocks SET active=FALSE, last_error=$2, updated_at=NOW() WHERE remote_ip=$1`, ip, errorString(operationErr))
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO security_audit_log (ts_utc, actor_user_id, action, remote_ip, detail, success) VALUES (NOW(), $1, $2, $3, $4, $5)`, userID, action, ip, detail, success)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
