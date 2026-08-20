package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// Config contains the runtime settings supplied to the application.
type Config struct {
	Addr          string
	DataDir       string
	SecureCookies bool
}

// Load reads and validates configuration from the environment.
func Load() (Config, error) {
	secure, err := envBool("STRATUM_SECURE_COOKIES")
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		Addr:          envOrDefault("STRATUM_ADDR", ":8080"),
		DataDir:       envOrDefault("STRATUM_DATA_DIR", "./data"),
		SecureCookies: secure,
	}
	if strings.TrimSpace(cfg.DataDir) == "" {
		return Config{}, fmt.Errorf("STRATUM_DATA_DIR must not be empty")
	}
	if _, err := net.ResolveTCPAddr("tcp", cfg.Addr); err != nil {
		return Config{}, fmt.Errorf("invalid STRATUM_ADDR %q: %w", cfg.Addr, err)
	}
	return cfg, nil
}

func envBool(key string) (bool, error) {
	value := os.Getenv(key)
	if value == "" {
		return false, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("invalid %s %q: expected a boolean", key, value)
	}
	return parsed, nil
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
