package main

import (
	"os"
	"strings"
)

func envOrDefault(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func isProduction() bool {
	return strings.EqualFold(envOrDefault("APP_ENV", "development"), "production")
}
