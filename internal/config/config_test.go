package config

import (
	"os"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	
	if cfg.DataDir != "./data" {
		t.Errorf("expected DataDir to be ./data, got %q", cfg.DataDir)
	}
	if cfg.Addr != "127.0.0.1:8080" {
		t.Errorf("expected Addr to be 127.0.0.1:8080, got %q", cfg.Addr)
	}
}

func TestLoadConfig(t *testing.T) {
	// Save original env vars
	oldDataDir := os.Getenv("FA_DATA_DIR")
	oldAddr := os.Getenv("FA_ADDR")
	oldLogLevel := os.Getenv("FA_LOG_LEVEL")
	
	// Clean up
	defer func() {
		os.Setenv("FA_DATA_DIR", oldDataDir)
		os.Setenv("FA_ADDR", oldAddr)
		os.Setenv("FA_LOG_LEVEL", oldLogLevel)
	}()

	// Test with custom values
	os.Setenv("FA_DATA_DIR", "/custom/data")
	os.Setenv("FA_ADDR", "0.0.0.0:9000")
	os.Setenv("FA_LOG_LEVEL", "debug")

	cfg := LoadConfig()

	if cfg.DataDir != "/custom/data" {
		t.Errorf("expected DataDir to be /custom/data, got %q", cfg.DataDir)
	}
	if cfg.Addr != "0.0.0.0:9000" {
		t.Errorf("expected Addr to be 0.0.0.0:9000, got %q", cfg.Addr)
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected string // String representation of slog.Level
		wantErr  bool
	}{
		{"debug", "DEBUG", false},
		{"info", "INFO", false},
		{"warn", "WARN", false},
		{"error", "ERROR", false},
		{"invalid", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			level, err := ParseLogLevel(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseLogLevel() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && level.String() != tt.expected {
				t.Errorf("ParseLogLevel() = %v, want %v", level.String(), tt.expected)
			}
		})
	}
}
