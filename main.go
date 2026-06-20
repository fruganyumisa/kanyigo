package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"
)

func main() {
	warnIfRunningAsRoot()

	var (
		mode       = flag.String("mode", "serve", "mode: follow, init, health, or serve")
		maillog    = flag.String("maillog", envOrDefault("MAILLOG_PATH", "/var/log/maillog"), "path to maillog")
		dbURL      = flag.String("db", envOrDefault("DATABASE_URL", "postgres://logs:logs@localhost:5432/logs_dashboard?sslmode=disable"), "postgres connection string")
		listenAddr = flag.String("listen", envOrDefault("LISTEN_ADDR", ":8080"), "http listen address")
	)
	flag.Parse()

	db, err := OpenDB(*dbURL)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if *mode == "health" {
		fmt.Println("ok")
		return
	}

	if err := EnsureSchema(db); err != nil {
		log.Fatalf("ensure schema: %v", err)
	}
	if err := EnsureBootstrapAdmin(db); err != nil {
		log.Fatalf("ensure bootstrap admin: %v", err)
	}

	switch *mode {
	case "init":
		fmt.Println("database initialized")
	case "follow":
		cfg, err := streamConfig(*maillog)
		if err != nil {
			log.Fatalf("ingest config: %v", err)
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		runMailLogIngestorWithRetry(ctx, db, cfg)
	case "serve":
		server := NewServer(db)
		go func() {
			if envOrDefault("AUTO_INGEST", "false") != "true" {
				return
			}
			cfg, err := streamConfig(*maillog)
			if err != nil {
				log.Printf("ingest config: %v", err)
				return
			}
			runMailLogIngestorWithRetry(context.Background(), db, cfg)
		}()
		go func() {
			if envOrDefault("AUTO_NGINX_SECURITY_INGEST", "false") != "true" {
				return
			}
			cfg, err := nginxSecurityConfig()
			if err != nil {
				log.Printf("nginx security ingest config: %v", err)
				return
			}
			runNginxSecurityIngestorWithRetry(context.Background(), db, cfg)
		}()
		log.Printf("listening on %s", *listenAddr)
		if err := server.ListenAndServe(*listenAddr); err != nil {
			log.Fatalf("serve: %v", err)
		}
	default:
		fmt.Fprintln(os.Stderr, "unknown mode")
		os.Exit(2)
	}
}

func nginxSecurityConfig() (NginxSecurityConfig, error) {
	pollInterval, err := time.ParseDuration(envOrDefault("NGINX_LOG_POLL_INTERVAL", "250ms"))
	if err != nil {
		return NginxSecurityConfig{}, fmt.Errorf("invalid NGINX_LOG_POLL_INTERVAL: %w", err)
	}
	rotationDrainTimeout, err := time.ParseDuration(envOrDefault("NGINX_LOG_ROTATION_DRAIN_TIMEOUT", "1s"))
	if err != nil {
		return NginxSecurityConfig{}, fmt.Errorf("invalid NGINX_LOG_ROTATION_DRAIN_TIMEOUT: %w", err)
	}
	consecutiveWindow, err := time.ParseDuration(envOrDefault("BRUTEFORCE_404_WINDOW", "2m"))
	if err != nil {
		return NginxSecurityConfig{}, fmt.Errorf("invalid BRUTEFORCE_404_WINDOW: %w", err)
	}
	authWindow, err := time.ParseDuration(envOrDefault("BRUTEFORCE_AUTH_WINDOW", "5m"))
	if err != nil {
		return NginxSecurityConfig{}, fmt.Errorf("invalid BRUTEFORCE_AUTH_WINDOW: %w", err)
	}
	trustedProxies, err := parseCIDRs(envOrDefault("TRUSTED_PROXY_CIDRS", "127.0.0.1/32,::1/128"))
	if err != nil {
		return NginxSecurityConfig{}, err
	}
	ignoredPaths := make(map[string]bool)
	for _, path := range strings.Split(envOrDefault("BRUTEFORCE_404_IGNORED_PATHS", "/favicon.ico,/robots.txt,/api/health"), ",") {
		if path = strings.TrimSpace(path); path != "" {
			ignoredPaths[path] = true
		}
	}
	cfg := NginxSecurityConfig{
		Path:                    envOrDefault("NGINX_ACCESS_LOG_PATH", "/var/log/nginx/access.log"),
		PollInterval:            pollInterval,
		RotationDrainTimeout:    rotationDrainTimeout,
		Consecutive404Threshold: envInt("BRUTEFORCE_404_CONSECUTIVE_THRESHOLD", 10),
		Consecutive404Window:    consecutiveWindow,
		AuthFailureThreshold:    envInt("BRUTEFORCE_AUTH_THRESHOLD", 10),
		AuthFailureWindow:       authWindow,
		IgnoredPaths:            ignoredPaths,
		TrustedProxyCIDRs:       trustedProxies,
	}
	if cfg.Consecutive404Threshold <= 0 || cfg.AuthFailureThreshold <= 0 || cfg.Consecutive404Window <= 0 || cfg.AuthFailureWindow <= 0 {
		return NginxSecurityConfig{}, errors.New("nginx security thresholds and windows must be positive")
	}
	return cfg, nil
}

func streamConfig(path string) (StreamConfig, error) {
	pollInterval, err := time.ParseDuration(envOrDefault("MAILLOG_POLL_INTERVAL", "250ms"))
	if err != nil {
		return StreamConfig{}, fmt.Errorf("invalid MAILLOG_POLL_INTERVAL: %w", err)
	}
	rotationDrainTimeout, err := time.ParseDuration(envOrDefault("MAILLOG_ROTATION_DRAIN_TIMEOUT", "1s"))
	if err != nil {
		return StreamConfig{}, fmt.Errorf("invalid MAILLOG_ROTATION_DRAIN_TIMEOUT: %w", err)
	}
	queueIdleTimeout, err := time.ParseDuration(envOrDefault("MAILLOG_QUEUE_IDLE_TIMEOUT", "30m"))
	if err != nil {
		return StreamConfig{}, fmt.Errorf("invalid MAILLOG_QUEUE_IDLE_TIMEOUT: %w", err)
	}
	return StreamConfig{
		Path:                 path,
		PollInterval:         pollInterval,
		RotationDrainTimeout: rotationDrainTimeout,
		QueueIdleTimeout:     queueIdleTimeout,
		ProcessingWorkers:    envInt("MAILLOG_PROCESSING_WORKERS", max(2, runtime.GOMAXPROCS(0))),
	}, nil
}

func warnIfRunningAsRoot() {
	if os.Geteuid() == 0 {
		log.Printf("warning: running as root is deprecated; provision maillog read access with POSIX ACLs or adm/log group membership")
	}
}

func runMailLogIngestorWithRetry(ctx context.Context, db *sql.DB, cfg StreamConfig) {
	for {
		if err := RunMailLogIngestor(ctx, db, cfg); err != nil {
			log.Printf("continuous maillog ingest: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func runNginxSecurityIngestorWithRetry(ctx context.Context, db *sql.DB, cfg NginxSecurityConfig) {
	for {
		if err := RunNginxSecurityIngestor(ctx, db, cfg); err != nil {
			log.Printf("continuous nginx security ingest: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}
