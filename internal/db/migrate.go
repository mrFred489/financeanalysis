// Package db provides database migration functionality.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrSchemaTooNew is returned when the database schema version is newer than what the binary supports.
var ErrSchemaTooNew = fmt.Errorf("database schema version is newer than the binary supports")

// CurrentSchemaVersion is the current schema version supported by this binary.
const CurrentSchemaVersion = 1

// Migrate runs all pending migrations on the database.
// For M0, we manually apply the initial schema since we only have one migration.
// If the database is at a higher version than the binary supports, it returns ErrSchemaTooNew.
// If there are pending migrations, it takes a backup first.
func Migrate(ctx context.Context, db *sql.DB, backupDir string) error {
	// Get current database version
	currentVersion, err := getDBVersion(db)
	if err != nil {
		// Database might not have the goose table yet - this is expected for a new database
		if isVersionNotFound(err) {
			currentVersion = 0
		} else {
			return fmt.Errorf("failed to get current version: %w", err)
		}
	}

	// Check if schema is too new
	if currentVersion > CurrentSchemaVersion {
		return fmt.Errorf("%w: database version %d, max supported %d", ErrSchemaTooNew, currentVersion, CurrentSchemaVersion)
	}

	// If we need to apply migrations
	if currentVersion < CurrentSchemaVersion {
		// Take a backup before migrating
		if err := Backup(ctx, db, backupDir, "pre-migrate"); err != nil {
			return fmt.Errorf("failed to create backup before migration: %w", err)
		}

		// Apply the initial schema
		if err := applyInitialSchema(ctx, db); err != nil {
			return fmt.Errorf("failed to apply initial schema: %w", err)
		}
	}

	// Run integrity checks
	if err := integrityCheck(ctx, db); err != nil {
		return fmt.Errorf("integrity check failed after migration: %w", err)
	}

	return nil
}

// applyInitialSchema applies the initial database schema for version 1.
func applyInitialSchema(ctx context.Context, db *sql.DB) error {
	// Create goose version table
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS goose_db_version (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			version_id INTEGER NOT NULL UNIQUE,
			is_applied BOOLEAN NOT NULL DEFAULT 1,
			tstamp TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now'))
		)
	`); err != nil {
		return fmt.Errorf("failed to create goose_db_version table: %w", err)
	}

	// Create setting table
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS setting (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("failed to create setting table: %w", err)
	}

	// Create category table
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS category (
			id INTEGER PRIMARY KEY,
			key TEXT UNIQUE NOT NULL,
			name TEXT NOT NULL,
			color TEXT NOT NULL
		) STRICT
	`); err != nil {
		return fmt.Errorf("failed to create category table: %w", err)
	}

	// Seed the category table with the four fixed categories
	if _, err := db.ExecContext(ctx, `
		INSERT INTO category (id, key, name, color) VALUES
			(1, 'income', 'Income', '#00b894'),
			(2, 'fixed', 'Fixed costs', '#e17055'),
			(3, 'variable', 'Variable spending', '#6c5ce7'),
			(4, 'savings', 'Savings', '#fd79a8')
		ON CONFLICT DO NOTHING
	`); err != nil {
		return fmt.Errorf("failed to seed category table: %w", err)
	}

	// Record this migration
	if _, err := db.ExecContext(ctx, `
		INSERT INTO goose_db_version (version_id, is_applied) VALUES (1, 1) ON CONFLICT DO NOTHING
	`); err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	return nil
}

// getDBVersion gets the current database version.
func getDBVersion(db *sql.DB) (int64, error) {
	var version int64
	err := db.QueryRow("SELECT version_id FROM goose_db_version ORDER BY id DESC LIMIT 1").Scan(&version)
	if err != nil {
		// If the table doesn't exist, that's fine - version is 0
		if strings.Contains(err.Error(), "no such table") {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to query version: %w", err)
	}
	return version, nil
}

// isVersionNotFound checks if the error indicates that the version table doesn't exist
func isVersionNotFound(err error) bool {
	return strings.Contains(err.Error(), "no such table")
}

// integrityCheck runs PRAGMA integrity_check and foreign_key_check.
func integrityCheck(ctx context.Context, db *sql.DB) error {
	// Check integrity
	var integrityErr string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrityErr); err != nil {
		return fmt.Errorf("failed to run integrity_check: %w", err)
	}
	if integrityErr != "ok" {
		return fmt.Errorf("integrity check failed: %s", integrityErr)
	}

	// Check foreign keys - this may return no rows, which is OK
	var fkErr sql.NullString
	if err := db.QueryRowContext(ctx, "PRAGMA foreign_key_check").Scan(&fkErr); err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to run foreign_key_check: %w", err)
	}
	if fkErr.Valid && fkErr.String != "" {
		return fmt.Errorf("foreign key check failed: %s", fkErr.String)
	}

	return nil
}

// Backup creates a database backup using VACUUM INTO.
func Backup(ctx context.Context, db *sql.DB, dir, reason string) error {
	// Ensure the backup directory exists
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory %q: %w", dir, err)
	}

	// Get the current database path
	dbPath := GetDBPath()
	if dbPath == "" {
		return fmt.Errorf("database path not set")
	}

	// Create backup filename with timestamp and reason
	timestamp := strings.ReplaceAll(filepath.Base(dbPath), ".sqlite", "")
	backupName := fmt.Sprintf("finance-%s-%s.sqlite", timestamp, reason)
	backupPath := filepath.Join(dir, backupName)

	// Run VACUUM INTO to create the backup
	if _, err := db.ExecContext(ctx, fmt.Sprintf("VACUUM INTO '%s'", backupPath)); err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	return nil
}

// RetainBackups keeps only the newest N backups in the directory.
func RetainBackups(dir string, maxKeep int) error {
	// List all backup files
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to list backups: %w", err)
	}

	// Filter and sort by modification time (newest first)
	var backups []struct {
		path string
		mtime int64
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".sqlite") {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			backups = append(backups, struct {
				path string
				mtime int64
			}{path: filepath.Join(dir, entry.Name()), mtime: info.ModTime().Unix()})
		}
	}

	// Sort by modification time (newest first)
	for i := 0; i < len(backups)-1; i++ {
		for j := i + 1; j < len(backups); j++ {
			if backups[j].mtime > backups[i].mtime {
				backups[i], backups[j] = backups[j], backups[i]
			}
		}
	}

	// Remove old backups
	if len(backups) > maxKeep {
		for _, backup := range backups[maxKeep:] {
			if err := os.Remove(backup.path); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to remove backup %q: %w", backup.path, err)
			}
		}
	}

	return nil
}
