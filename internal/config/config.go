// Package config reads runtime configuration from the environment.
package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Port           string
	DatabaseURL    string
	CyclePassword  string
	CookieSecure   bool
	SessionMaxDays int
}

func Load() (*Config, error) {
	port := os.Getenv("PORT")
	if port == "" {
		port = "4715"
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	password := os.Getenv("CYCLE_PASSWORD")
	if password == "" {
		return nil, fmt.Errorf("CYCLE_PASSWORD is required")
	}

	cookieSecure := true
	if v := os.Getenv("COOKIE_SECURE"); v != "" {
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("COOKIE_SECURE must be a bool: %w", err)
		}
		cookieSecure = parsed
	}

	return &Config{
		Port:           port,
		DatabaseURL:    dbURL,
		CyclePassword:  password,
		CookieSecure:   cookieSecure,
		SessionMaxDays: 90,
	}, nil
}
