package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
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
		log.Printf("listening on %s", *listenAddr)
		if err := server.ListenAndServe(*listenAddr); err != nil {
			log.Fatalf("serve: %v", err)
		}
	default:
		fmt.Fprintln(os.Stderr, "unknown mode")
		os.Exit(2)
	}
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
	}, nil
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
