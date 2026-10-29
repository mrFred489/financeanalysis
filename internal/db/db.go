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
	initDB     sync.Once
	initDBErr  error
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

// Close closes the global database connection.
func Close() error {
	if globalDB != nil {
		return globalDB.Close()
	}
	return nil
}

// GetDB returns the global database connection.
// It must be initialized via InitDB first.
func GetDB() (*sql.DB, error) {
	if initDBErr != nil {
		return nil, fmt.Errorf("database not initialized: %w", initDBErr)
	}
	return globalDB, nil
}

// InitDB initializes the global database connection.
// It should be called once during application startup.
func InitDB(ctx context.Context, path string) error {
	initDB.Do(func() {
		var err error
		globalDB, err = Open(ctx, path)
		if err != nil {
			initDBErr = err
			return
		}
		globalPath = path
	})
	return initDBErr
}

// GetDBPath returns the path of the global database.
func GetDBPath() string {
	return globalPath
}
