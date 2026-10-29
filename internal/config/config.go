// Package config provides application configuration from environment variables.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
)

// Config holds the application configuration.
type Config struct {
	DataDir  string
	Addr     string
	LogLevel slog.Level
}

// DefaultConfig returns configuration with sensible defaults.
func DefaultConfig() Config {
	return Config{
		DataDir:  "./data",
		Addr:     "127.0.0.1:8080",
		LogLevel: slog.LevelInfo,
	}
}

// LoadConfig loads configuration from environment variables.
// It falls back to defaults for any unset values.
func LoadConfig() Config {
	cfg := DefaultConfig()

	if v := os.Getenv("FA_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}

	if v := os.Getenv("FA_ADDR"); v != "" {
		cfg.Addr = v
	}

	if v := os.Getenv("FA_LOG_LEVEL"); v != "" {
		level := slog.LevelInfo
		if err := level.UnmarshalText([]byte(v)); err != nil {
			fmt.Fprintf(os.Stderr, "warning: invalid FA_LOG_LEVEL %q: %v\n", v, err)
		} else {
			cfg.LogLevel = level
		}
	}

	return cfg
}

// ParseLogLevel parses a log level string.
func ParseLogLevel(s string) (slog.Level, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(s)); err != nil {
		return 0, fmt.Errorf("invalid log level %q: %w", s, err)
	}
	return level, nil
}

// MustParseLogLevel parses a log level string or panics.
func MustParseLogLevel(s string) slog.Level {
	level, err := ParseLogLevel(s)
	if err != nil {
		panic(err)
	}
	return level
}

// ParseBool parses a boolean string from environment.
func ParseBool(s string) bool {
	b, err := strconv.ParseBool(s)
	if err != nil {
		return false
	}
	return b
}
