// Package config reads runtime configuration from the environment.
package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
)

type Config struct {
	Port           string
	DatabaseURL    string
	Password       string
	CookieSecure   bool
	SessionMaxDays int
}

func Load() (*Config, error) {
	// VARDE_PORT wins; PORT stays supported without a warning — it is the
	// Railway convention and gets injected automatically.
	port := os.Getenv("VARDE_PORT")
	if port == "" {
		port = os.Getenv("PORT")
	}
	if port == "" {
		port = "4715"
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	password := os.Getenv("VARDE_PASSWORD")
	if password == "" {
		if legacy := os.Getenv("CYCLE_PASSWORD"); legacy != "" {
			log.Printf("WARNING: CYCLE_PASSWORD is deprecated since the rename to Varde; set VARDE_PASSWORD instead")
			password = legacy
		}
	}
	if password == "" {
		return nil, fmt.Errorf("VARDE_PASSWORD is required")
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
		Password:       password,
		CookieSecure:   cookieSecure,
		SessionMaxDays: 90,
	}, nil
}
