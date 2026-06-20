package main

import (
	"database/sql"
	"os"
	"testing"
	"time"
)

func TestNginxConsecutive404DetectionIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := OpenDB(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := EnsureSchema(db); err != nil {
		t.Fatal(err)
	}

	ip := "192.0.2.240"
	cleanupSecurityTestIP(t, db, ip)
	defer cleanupSecurityTestIP(t, db, ip)
	cfg := NginxSecurityConfig{
		Consecutive404Threshold: 10,
		Consecutive404Window:    2 * time.Minute,
		AuthFailureThreshold:    10,
		AuthFailureWindow:       5 * time.Minute,
	}
	started := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 9; i++ {
		applyDetectionEvent(t, db, &NginxEvent{Timestamp: started.Add(time.Duration(i) * time.Second), RemoteIP: ip, Path: "/scan", Status: 404}, cfg)
	}
	assertOffenderCount(t, db, ip, 0)
	applyDetectionEvent(t, db, &NginxEvent{Timestamp: started.Add(10 * time.Second), RemoteIP: ip, Path: "/", Status: 200}, cfg)
	for i := 0; i < 10; i++ {
		applyDetectionEvent(t, db, &NginxEvent{Timestamp: started.Add(time.Minute + time.Duration(i)*time.Second), RemoteIP: ip, Path: "/scan", Status: 404}, cfg)
	}
	assertOffenderCount(t, db, ip, 1)
	var attempts int
	if err := db.QueryRow(`SELECT attempt_count FROM security_offenders WHERE remote_ip=$1`, ip).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 10 {
		t.Fatalf("attempt count: got %d want 10", attempts)
	}
}

func applyDetectionEvent(t *testing.T, db *sql.DB, event *NginxEvent, cfg NginxSecurityConfig) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := updateNginxDetection(tx, event, cfg); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func assertOffenderCount(t *testing.T, db *sql.DB, ip string, want int) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM security_offenders WHERE remote_ip=$1`, ip).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("offender count: got %d want %d", count, want)
	}
}

func cleanupSecurityTestIP(t *testing.T, db *sql.DB, ip string) {
	t.Helper()
	if _, err := db.Exec(`DELETE FROM security_offenders WHERE remote_ip=$1; DELETE FROM nginx_ip_detection_state WHERE remote_ip=$1;`, ip); err != nil {
		t.Fatal(err)
	}
}
