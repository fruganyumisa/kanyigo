package main

import (
	"net"
	"testing"
	"time"
)

func TestParseNginxJSON(t *testing.T) {
	cfg := testNginxConfig(t)
	line := `{"time_iso8601":"2026-06-20T12:30:45+03:00","remote_addr":"203.0.113.9","request_method":"GET","uri":"/.env?download=1","status":404,"http_user_agent":"scanner"}`
	event, ok := ParseNginxLine(line, cfg)
	if !ok {
		t.Fatal("expected JSON line to parse")
	}
	if event.RemoteIP != "203.0.113.9" || event.Method != "GET" || event.Path != "/.env" || event.Status != 404 {
		t.Fatalf("unexpected event: %+v", event)
	}
	want := time.Date(2026, 6, 20, 9, 30, 45, 0, time.UTC)
	if !event.Timestamp.Equal(want) {
		t.Fatalf("timestamp: got %s want %s", event.Timestamp, want)
	}
}

func TestParseNginxCombined(t *testing.T) {
	cfg := testNginxConfig(t)
	line := `198.51.100.4 - - [20/Jun/2026:12:31:00 +0300] "POST /wp-login.php HTTP/1.1" 403 120 "-" "bot"`
	event, ok := ParseNginxLine(line, cfg)
	if !ok {
		t.Fatal("expected combined line to parse")
	}
	if event.RemoteIP != "198.51.100.4" || event.Method != "POST" || event.Path != "/wp-login.php" || event.Status != 403 {
		t.Fatalf("unexpected event: %+v", event)
	}
}

func TestParseNginxUsesForwardedIPOnlyForTrustedProxy(t *testing.T) {
	cfg := testNginxConfig(t)
	trusted := `{"time_iso8601":"2026-06-20T12:30:45Z","remote_addr":"127.0.0.1","http_x_forwarded_for":"198.51.100.8, 10.0.0.2","request":"GET /admin HTTP/1.1","status":"404"}`
	event, ok := ParseNginxLine(trusted, cfg)
	if !ok || event.RemoteIP != "198.51.100.8" {
		t.Fatalf("trusted proxy event: %+v ok=%v", event, ok)
	}

	untrusted := `{"time_iso8601":"2026-06-20T12:30:45Z","remote_addr":"203.0.113.10","http_x_forwarded_for":"198.51.100.8","request":"GET /admin HTTP/1.1","status":404}`
	event, ok = ParseNginxLine(untrusted, cfg)
	if !ok || event.RemoteIP != "203.0.113.10" {
		t.Fatalf("untrusted proxy event: %+v ok=%v", event, ok)
	}
}

func TestParseNginxRejectsMalformedInput(t *testing.T) {
	if event, ok := ParseNginxLine("not an nginx line", testNginxConfig(t)); ok || event != nil {
		t.Fatalf("unexpected event: %+v", event)
	}
}

func TestParseCIDRsRejectsInvalidNetwork(t *testing.T) {
	if _, err := parseCIDRs("not-a-cidr"); err == nil {
		t.Fatal("expected invalid CIDR error")
	}
}

func testNginxConfig(t *testing.T) NginxSecurityConfig {
	t.Helper()
	_, loopback, err := net.ParseCIDR("127.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	_, internalProxy, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	return NginxSecurityConfig{TrustedProxyCIDRs: []*net.IPNet{loopback, internalProxy}}
}
