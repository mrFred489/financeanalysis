// Package db provides database connection management.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

var (
	mu         sync.RWMutex
	globalDB   *sql.DB
	globalPath string
)

// Open opens a SQLite database at the given path.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	// Ensure the directory exists
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create data directory %q: %w", dir, err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure the connection
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return nil, fmt.Errorf("failed to set foreign_keys: %w", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode = DELETE"); err != nil {
		return nil, fmt.Errorf("failed to set journal_mode: %w", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA synchronous = FULL"); err != nil {
		return nil, fmt.Errorf("failed to set synchronous: %w", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		return nil, fmt.Errorf("failed to set busy_timeout: %w", err)
	}

	// Limit connections to 1 for a single-user, single-file setup
	db.SetMaxOpenConns(1)

	return db, nil
}

// InitDB opens the database at the given path and makes it the global handle.
func InitDB(ctx context.Context, path string) error {
	d, err := Open(ctx, path)
	if err != nil {
		return err
	}
	SwapDB(d, path)
	return nil
}

// SwapDB replaces the global database handle, closing the previous one.
func SwapDB(d *sql.DB, path string) {
	mu.Lock()
	old := globalDB
	globalDB = d
	globalPath = path
	mu.Unlock()
	if old != nil && old != d {
		old.Close()
	}
}

// Close closes the global database connection.
func Close() error {
	mu.Lock()
	defer mu.Unlock()
	if globalDB != nil {
		err := globalDB.Close()
		globalDB = nil
		globalPath = ""
		return err
	}
	return nil
}

// GetDB returns the global database connection.
// It must be initialized via InitDB first.
func GetDB() (*sql.DB, error) {
	mu.RLock()
	defer mu.RUnlock()
	if globalDB == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	return globalDB, nil
}

// GetDBPath returns the path of the global database.
func GetDBPath() string {
	mu.RLock()
	defer mu.RUnlock()
	return globalPath
}
