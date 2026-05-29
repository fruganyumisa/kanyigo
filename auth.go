package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookieName = "logs_session"
	sessionTTL        = 7 * 24 * time.Hour
)

type contextKey string

const userContextKey contextKey = "authUser"

type authUser struct {
	ID        int64  `json:"id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	CreatedAt string `json:"createdAt,omitempty"`
}

func EnsureBootstrapAdmin(db *sql.DB) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM dashboard_users WHERE role = 'admin'`).Scan(&count); err != nil {
		return fmt.Errorf("count admins: %w", err)
	}
	if count > 0 {
		return nil
	}

	email := strings.TrimSpace(envOrDefault("ADMIN_EMAIL", ""))
	password := envOrDefault("ADMIN_PASSWORD", "")
	if !isProduction() {
		email = envOrDefault("ADMIN_EMAIL", "admin@example.com")
		password = envOrDefault("ADMIN_PASSWORD", "admin12345")
	}
	if email == "" || password == "" {
		if isProduction() {
			return errors.New("ADMIN_EMAIL and ADMIN_PASSWORD are required when no admin user exists")
		}
		return nil
	}
	if isProduction() && (email == "admin@example.com" || password == "admin12345" || len(password) < 12) {
		return errors.New("production ADMIN_PASSWORD must be at least 12 characters and must not use default credentials")
	}
	if _, err := createUser(db, email, password, "admin"); err != nil {
		return fmt.Errorf("create bootstrap admin: %w", err)
	}
	return nil
}

func createUser(db *sql.DB, email string, password string, role string) (authUser, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return authUser{}, err
	}
	if len(password) < 8 {
		return authUser{}, errors.New("password must be at least 8 characters")
	}
	if role != "admin" && role != "user" {
		return authUser{}, errors.New("role must be admin or user")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return authUser{}, err
	}

	var user authUser
	var createdAt time.Time
	err = db.QueryRow(`
INSERT INTO dashboard_users (email, password_hash, role)
VALUES ($1, $2, $3)
RETURNING id, email, role, created_at;
`, email, string(hash), role).Scan(&user.ID, &user.Email, &user.Role, &createdAt)
	if err != nil {
		return authUser{}, err
	}
	user.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	return user, nil
}

func normalizeEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "", errors.New("email is required")
	}
	if _, err := mail.ParseAddress(value); err != nil {
		return "", errors.New("email is invalid")
	}
	return value, nil
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	_, _ = s.db.Exec(`DELETE FROM dashboard_sessions WHERE expires_at <= NOW()`)

	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid JSON body"))
		return
	}

	email, err := normalizeEmail(req.Email)
	if err != nil {
		writeError(w, http.StatusUnauthorized, errors.New("invalid email or password"))
		return
	}

	var user authUser
	var passwordHash string
	var createdAt time.Time
	err = s.db.QueryRow(`
SELECT id, email, role, password_hash, created_at
FROM dashboard_users
WHERE email = $1;
`, email).Scan(&user.ID, &user.Email, &user.Role, &passwordHash, &createdAt)
	if err != nil {
		writeError(w, http.StatusUnauthorized, errors.New("invalid email or password"))
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, errors.New("invalid email or password"))
		return
	}
	user.CreatedAt = createdAt.UTC().Format(time.RFC3339)

	token, err := randomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	expiresAt := time.Now().UTC().Add(sessionTTL)
	if _, err := s.db.Exec(`
INSERT INTO dashboard_sessions (token_hash, user_id, expires_at)
VALUES ($1, $2, $3);
`, hashToken(token), user.ID, expiresAt); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	http.SetCookie(w, sessionCookie(r, token, expiresAt))
	writeJSON(w, http.StatusOK, map[string]authUser{"user": user})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	if cookie, err := r.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
		_, _ = s.db.Exec(`DELETE FROM dashboard_sessions WHERE token_hash = $1`, hashToken(cookie.Value))
	}
	http.SetCookie(w, expiredSessionCookie(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	user, ok := currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, errors.New("authentication required"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]authUser{"user": user})
}

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, errors.New("authentication required"))
		return
	}
	if user.Role != "admin" {
		writeError(w, http.StatusForbidden, errors.New("admin access required"))
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.listUsers(w, r)
	case http.MethodPost:
		s.createUser(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
	}
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
SELECT id, email, role, created_at
FROM dashboard_users
ORDER BY created_at DESC, id DESC;
`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	users := []authUser{}
	for rows.Next() {
		var user authUser
		var createdAt time.Time
		if err := rows.Scan(&user.ID, &user.Email, &user.Role, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		user.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]authUser{"items": users})
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid JSON body"))
		return
	}
	user, err := createUser(s.db, req.Email, req.Password, req.Role)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]authUser{"user": user})
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := s.authenticateRequest(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, errors.New("authentication required"))
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func (s *Server) authenticateRequest(r *http.Request) (authUser, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return authUser{}, errors.New("missing session")
	}

	var user authUser
	var createdAt time.Time
	err = s.db.QueryRow(`
SELECT u.id, u.email, u.role, u.created_at
FROM dashboard_sessions s
JOIN dashboard_users u ON u.id = s.user_id
WHERE s.token_hash = $1 AND s.expires_at > NOW();
`, hashToken(cookie.Value)).Scan(&user.ID, &user.Email, &user.Role, &createdAt)
	if err != nil {
		return authUser{}, err
	}
	user.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	return user, nil
}

func currentUser(r *http.Request) (authUser, bool) {
	user, ok := r.Context().Value(userContextKey).(authUser)
	return user, ok
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func sessionCookie(r *http.Request, token string, expiresAt time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
	}
}

func expiredSessionCookie(r *http.Request) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
	}
}

func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
