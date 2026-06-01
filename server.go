package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Server struct {
	db             *sql.DB
	allowedOrigins map[string]bool
}

func NewServer(db *sql.DB) *Server {
	return &Server{
		db:             db,
		allowedOrigins: parseAllowedOrigins(envOrDefault("ALLOWED_ORIGINS", "http://localhost:3000")),
	}
}

func (s *Server) ListenAndServe(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/auth/login", s.handleLogin)
	mux.HandleFunc("/api/auth/logout", s.handleLogout)
	mux.HandleFunc("/api/auth/me", s.requireAuth(s.handleMe))
	mux.HandleFunc("/api/users", s.requireAuth(s.handleUsers))
	mux.HandleFunc("/api/logs", s.requireAuth(s.handleLogs))
	server := &http.Server{
		Addr:              addr,
		Handler:           s.withCORS(withSecurityHeaders(withLogging(mux))),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return server.ListenAndServe()
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

type logsResponse struct {
	Total int         `json:"total"`
	Items []logRecord `json:"items"`
}

type logRecord struct {
	ID           int64    `json:"id"`
	TSUTC        string   `json:"tsUtc"`
	From         string   `json:"from"`
	To           string   `json:"to"`
	Status       string   `json:"status"`
	Host         string   `json:"host"`
	Process      string   `json:"process"`
	QueueID      string   `json:"queueId"`
	Relay        string   `json:"relay"`
	Delay        *float64 `json:"delay"`
	Delays       string   `json:"delays"`
	DSN          string   `json:"dsn"`
	MessageID    string   `json:"messageId"`
	SizeBytes    *int64   `json:"sizeBytes"`
	QueuedAs     string   `json:"queuedAs"`
	MailID       string   `json:"mailId"`
	Subject      string   `json:"subject"`
	Hits         *float64 `json:"hits"`
	Helo         string   `json:"helo"`
	AmavisOrigin string   `json:"amavisOrigin"`
	IsJunk       bool     `json:"isJunk"`
	SpamScore    *float64 `json:"spamScore"`
	TimedOut     bool     `json:"timedOut"`
	Raw          string   `json:"raw"`
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from := q.Get("from")
	to := q.Get("to")
	sender := q.Get("sender")
	receiver := q.Get("receiver")
	status := q.Get("status")
	search := q.Get("q")
	limit := parseIntDefault(q.Get("limit"), 100)
	offset := parseIntDefault(q.Get("offset"), 0)
	if limit > 500 {
		limit = 500
	}

	where := []string{"1=1"}
	args := []interface{}{}
	addArg := func(v interface{}) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}

	if from != "" {
		where = append(where, "arrival_ts_utc >= "+addArg(normalizeTime(from)))
	}
	if to != "" {
		where = append(where, "arrival_ts_utc <= "+addArg(normalizeTime(to)))
	}
	if sender != "" {
		where = append(where, "mail_from ILIKE "+addArg("%"+sender+"%"))
	}
	if receiver != "" {
		where = append(where, "mail_to ILIKE "+addArg("%"+receiver+"%"))
	}
	if status != "" {
		where = append(where, "status = "+addArg(status))
	}
	if search != "" {
		where = append(where, "raw ILIKE "+addArg("%"+search+"%"))
	}

	whereSQL := strings.Join(where, " AND ")

	var total int
	countSQL := "SELECT COUNT(*) FROM mail_transactions WHERE " + whereSQL
	if err := s.db.QueryRow(countSQL, args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	querySQL := `
SELECT id, arrival_ts_utc, mail_from, mail_to, status, host, process, queue_id, relay, delay, delays, dsn, message_id, size_bytes, queued_as, mail_id, subject, hits, helo, amavis_origin, is_junk, spam_score, timed_out, raw
FROM mail_transactions
WHERE ` + whereSQL + `
ORDER BY arrival_ts_utc DESC
LIMIT ` + addArg(limit) + ` OFFSET ` + addArg(offset) + `;`

	rows, err := s.db.Query(querySQL, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	items := []logRecord{}
	for rows.Next() {
		var rec logRecord
		var ts time.Time
		var delay sql.NullFloat64
		var size sql.NullInt64
		var hits sql.NullFloat64
		var spamScore sql.NullFloat64
		var queuedAs sql.NullString
		var mailID sql.NullString
		var subject sql.NullString
		var helo sql.NullString
		var amavisOrigin sql.NullString
		if err := rows.Scan(
			&rec.ID, &ts, &rec.From, &rec.To, &rec.Status, &rec.Host, &rec.Process, &rec.QueueID,
			&rec.Relay, &delay, &rec.Delays, &rec.DSN, &rec.MessageID, &size, &queuedAs, &mailID, &subject, &hits, &helo, &amavisOrigin, &rec.IsJunk, &spamScore, &rec.TimedOut, &rec.Raw,
		); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		rec.TSUTC = ts.UTC().Format(time.RFC3339)
		if delay.Valid {
			rec.Delay = &delay.Float64
		}
		if size.Valid {
			rec.SizeBytes = &size.Int64
		}
		if hits.Valid {
			rec.Hits = &hits.Float64
		}
		if spamScore.Valid {
			rec.SpamScore = &spamScore.Float64
		}
		if queuedAs.Valid {
			rec.QueuedAs = queuedAs.String
		}
		if mailID.Valid {
			rec.MailID = mailID.String
		}
		if subject.Valid {
			rec.Subject = subject.String
		}
		if helo.Valid {
			rec.Helo = helo.String
		}
		if amavisOrigin.Valid {
			rec.AmavisOrigin = amavisOrigin.String
		}
		items = append(items, rec)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(logsResponse{Total: total, Items: items})
}

func parseIntDefault(v string, def int) int {
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil || i < 0 {
		return def
	}
	return i
}

func normalizeTime(v string) interface{} {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse("2006-01-02 15:04:05", v); err == nil {
		return t.UTC()
	}
	return v
}

func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if !s.allowedOrigins[origin] {
				writeError(w, http.StatusForbidden, errors.New("origin not allowed"))
				return
			}
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func parseAllowedOrigins(value string) map[string]bool {
	allowed := map[string]bool{}
	for _, part := range strings.Split(value, ",") {
		origin := strings.TrimSpace(part)
		if origin != "" {
			allowed[origin] = true
		}
	}
	return allowed
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		ip := clientIP(r)
		log.Printf("%s %s %d %s", ip, r.Method, rec.status, r.URL.Path)
	})
}

func clientIP(r *http.Request) string {
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	xri := strings.TrimSpace(r.Header.Get("X-Real-IP"))
	if xri != "" {
		return xri
	}
	host := r.RemoteAddr
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		return host[:idx]
	}
	return host
}
