package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Server struct {
	db *sql.DB
}

func NewServer(db *sql.DB) *Server {
	return &Server{db: db}
}

func (s *Server) ListenAndServe(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/logs", s.handleLogs)
	return http.ListenAndServe(addr, withCORS(withLogging(mux)))
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
	DSN          string   `json:"dsn"`
	MessageID    string   `json:"messageId"`
	SizeBytes    *int64   `json:"sizeBytes"`
	QueuedAs     string   `json:"queuedAs"`
	MailID       string   `json:"mailId"`
	Subject      string   `json:"subject"`
	Hits         *float64 `json:"hits"`
	Helo         string   `json:"helo"`
	AmavisOrigin string   `json:"amavisOrigin"`
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

	where := []string{"1=1"}
	args := []interface{}{}

	if from != "" {
		where = append(where, "ts_utc >= ?")
		args = append(args, normalizeTime(from))
	}
	if to != "" {
		where = append(where, "ts_utc <= ?")
		args = append(args, normalizeTime(to))
	}
	if sender != "" {
		where = append(where, "mail_from LIKE ?")
		args = append(args, "%"+sender+"%")
	}
	if receiver != "" {
		where = append(where, "mail_to LIKE ?")
		args = append(args, "%"+receiver+"%")
	}
	if status != "" {
		where = append(where, "status = ?")
		args = append(args, status)
	}
	if search != "" {
		where = append(where, "raw LIKE ?")
		args = append(args, "%"+search+"%")
	}

	whereSQL := strings.Join(where, " AND ")

	var total int
	countSQL := "SELECT COUNT(*) FROM maillog_entries WHERE " + whereSQL
	if err := s.db.QueryRow(countSQL, args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	querySQL := `
SELECT id, ts_utc, mail_from, mail_to, status, host, process, queue_id, relay, delay, dsn, message_id, size_bytes, queued_as, mail_id, subject, hits, helo, amavis_origin, raw
FROM maillog_entries
WHERE ` + whereSQL + `
ORDER BY ts_utc DESC
LIMIT ? OFFSET ?;`

	args = append(args, limit, offset)
	rows, err := s.db.Query(querySQL, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	items := []logRecord{}
	for rows.Next() {
		var rec logRecord
		var delay sql.NullFloat64
		var size sql.NullInt64
		var hits sql.NullFloat64
		var queuedAs sql.NullString
		var mailID sql.NullString
		var subject sql.NullString
		var helo sql.NullString
		var amavisOrigin sql.NullString
		if err := rows.Scan(
			&rec.ID, &rec.TSUTC, &rec.From, &rec.To, &rec.Status, &rec.Host, &rec.Process, &rec.QueueID,
			&rec.Relay, &delay, &rec.DSN, &rec.MessageID, &size, &queuedAs, &mailID, &subject, &hits, &helo, &amavisOrigin, &rec.Raw,
		); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if delay.Valid {
			rec.Delay = &delay.Float64
		}
		if size.Valid {
			rec.SizeBytes = &size.Int64
		}
		if hits.Valid {
			rec.Hits = &hits.Float64
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

func normalizeTime(v string) string {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UTC().Format(time.RFC3339)
	}
	if t, err := time.Parse("2006-01-02 15:04:05", v); err == nil {
		return t.UTC().Format(time.RFC3339)
	}
	return v
}

func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
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
