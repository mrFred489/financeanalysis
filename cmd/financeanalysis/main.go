// financeanalysis is a locally hosted web application for cataloging and visualizing personal spending.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mrFred489/financeanalysis/internal/config"
	"github.com/mrFred489/financeanalysis/internal/db"
	"github.com/mrFred489/financeanalysis/internal/server"
)

var version = "dev"

func main() {
	// Parse command line flags
	serveCmd := flag.NewFlagSet("serve", flag.ExitOnError)

	if len(os.Args) < 2 {
		// Default to serve command
		os.Args = append([]string{"financeanalysis"}, os.Args...)
		os.Args = append(os.Args, "serve")
	}

	switch os.Args[1] {
	case "serve":
		serveCmd.Parse(os.Args[2:])
		if err := runServe(); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "healthcheck":
		if err := runHealthcheck(); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Usage: financeanalysis <serve|healthcheck>\n")
		os.Exit(1)
	}
}

func runServe() error {
	// Load configuration
	cfg := config.LoadConfig()

	// Set up logging
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))
	slog.SetDefault(logger)

	// Initialize database
	ctx := context.Background()
	dataDir := cfg.DataDir
	dbPath := filepath.Join(dataDir, "finance.sqlite")
	backupDir := filepath.Join(dataDir, "backups")

	// Create data directory if it doesn't exist
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory %q: %w", dataDir, err)
	}

	// Create backup directory if it doesn't exist
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory %q: %w", backupDir, err)
	}

	// Check if database exists
	dbExists := false
	if _, err := os.Stat(dbPath); err == nil {
		dbExists = true
	}

	// Initialize and migrate database
	if err := db.InitDB(ctx, dbPath); err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}

	if dbExists {
		if err := db.Migrate(ctx, nil, backupDir); err != nil {
			if errors.Is(err, db.ErrSchemaTooNew) {
				return fmt.Errorf("fatal: %w. Please upgrade the application.", err)
			}
			return fmt.Errorf("failed to migrate database: %w", err)
		}
	}

	// Create HTTP server
	srv, err := server.NewServer(cfg.Addr, backupDir)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	// Start server in a goroutine
	go func() {
		slog.Info("starting server", "addr", cfg.Addr, "version", version, "data_dir", dataDir)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "error", err)
		}
	}()

	// Handle graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server")

	// Shutdown server with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server shutdown error", "error", err)
	}

	// Close database
	if err := db.Close(); err != nil {
		slog.Error("database close error", "error", err)
	}

	slog.Info("server stopped")
	return nil
}

func runHealthcheck() error {
	cfg := config.LoadConfig()

	// Try to reach the status endpoint
	url := fmt.Sprintf("http://%s/api/status", cfg.Addr)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach healthcheck endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck failed with status %d", resp.StatusCode)
	}

	fmt.Println("OK")
	return nil
}
