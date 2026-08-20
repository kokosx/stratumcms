package config

import (
	"os"
	"strconv"
)

// Config contains the runtime settings supplied to the application.
type Config struct {
	Addr          string
	DataDir       string
	SecureCookies bool
}

// Load reads configuration from the environment.
func Load() Config {
	return Config{
		Addr:          envOrDefault("STRATUM_ADDR", ":8080"),
		DataDir:       envOrDefault("STRATUM_DATA_DIR", "./data"),
		SecureCookies: envBool("STRATUM_SECURE_COOKIES"),
	}
}

func envBool(key string) bool {
	value, err := strconv.ParseBool(os.Getenv(key))
	return err == nil && value
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
