// Package config loads runtime settings from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds all runtime settings.
type Config struct {
	Port        string
	DatabaseURL string
	// AllowedOrigins for CORS and WebSocket origin checks. ["*"] allows any.
	AllowedOrigins []string
	// APIKey, when non-empty, is required on every request (X-API-Key header,
	// or ?api_key= for the WebSocket). Empty disables auth.
	APIKey string
	// Per-vehicle rate limit on POST /vehicles/location. RPS <= 0 disables it.
	LocationRPS   float64
	LocationBurst int
}

// Load reads the environment.
func Load() (Config, error) {
	c := Config{
		Port:           env("PORT", "8080"),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		AllowedOrigins: splitList(env("CORS_ALLOWED_ORIGINS", "*")),
		APIKey:         os.Getenv("API_KEY"),
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL is required")
	}
	var err error
	if c.LocationRPS, err = strconv.ParseFloat(env("RATE_LIMIT_RPS", "5"), 64); err != nil {
		return c, fmt.Errorf("RATE_LIMIT_RPS: %w", err)
	}
	if c.LocationBurst, err = strconv.Atoi(env("RATE_LIMIT_BURST", "10")); err != nil {
		return c, fmt.Errorf("RATE_LIMIT_BURST: %w", err)
	}
	return c, nil
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimRight(strings.TrimSpace(part), "/"); p != "" {
			out = append(out, p)
		}
	}
	return out
}
