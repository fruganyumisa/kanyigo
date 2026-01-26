package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"
)

func main() {
	var (
		mode       = flag.String("mode", "serve", "mode: ingest or serve")
		maillog    = flag.String("maillog", envOrDefault("MAILLOG_PATH", "/var/log/maillog"), "path to maillog")
		dbPath     = flag.String("db", envOrDefault("DB_PATH", "./maillog.db"), "path to sqlite db")
		listenAddr = flag.String("listen", envOrDefault("LISTEN_ADDR", ":8080"), "http listen address")
	)
	flag.Parse()

	db, err := OpenDB(*dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := EnsureSchema(db); err != nil {
		log.Fatalf("ensure schema: %v", err)
	}

	switch *mode {
	case "init":
		fmt.Println("database initialized")
	case "ingest":
		stats, err := IngestFile(db, *maillog)
		if err != nil {
			log.Fatalf("ingest: %v", err)
		}
		fmt.Printf("ingested=%d skipped=%d errors=%d\n", stats.Inserted, stats.Skipped, stats.Errors)
	case "tail":
		stats, err := IngestIncremental(db, *maillog)
		if err != nil {
			log.Fatalf("ingest: %v", err)
		}
		fmt.Printf("ingested=%d skipped=%d errors=%d\n", stats.Inserted, stats.Skipped, stats.Errors)
	case "follow":
		interval, err := time.ParseDuration(envOrDefault("INGEST_INTERVAL", "2m"))
		if err != nil {
			log.Fatalf("invalid INGEST_INTERVAL: %v", err)
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			stats, err := IngestIncremental(db, *maillog)
			if err != nil {
				log.Printf("ingest: %v", err)
			} else if stats.Inserted > 0 || stats.Errors > 0 {
				log.Printf("ingested=%d skipped=%d errors=%d", stats.Inserted, stats.Skipped, stats.Errors)
			}
			<-ticker.C
		}
	case "serve":
		server := NewServer(db)
		go func() {
			// optional periodic ingest when running in serve mode
			if envOrDefault("AUTO_INGEST", "false") != "true" {
				return
			}
			interval, err := time.ParseDuration(envOrDefault("INGEST_INTERVAL", "2m"))
			if err != nil {
				log.Printf("invalid INGEST_INTERVAL: %v", err)
				return
			}
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for range ticker.C {
				if _, err := IngestIncremental(db, *maillog); err != nil {
					log.Printf("ingest: %v", err)
				}
			}
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

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
