// Package db provides database migration functionality.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var embedMigrations embed.FS

// ErrSchemaTooNew is returned when the database schema version is newer than what the binary supports.
var ErrSchemaTooNew = fmt.Errorf("database schema version is newer than the binary supports")

// MaxMigrationVersion is the highest migration version available.
// This should be updated whenever new migrations are added.
const MaxMigrationVersion = 1

// Migrate runs all pending migrations on the database.
// If the database is at a higher version than the binary supports, it returns ErrSchemaTooNew.
// If there are pending migrations, it takes a backup first.
func Migrate(ctx context.Context, db *sql.DB, backupDir string) error {
	// Get current database version
	currentVersion, err := goose.GetDBVersionContext(ctx, db)
	if err != nil {
		// Database might not have the goose table yet
		if err == goose.ErrVersionNotFound {
			currentVersion = 0
		} else {
			return fmt.Errorf("failed to get current version: %w", err)
		}
	}

	// Check if schema is too new
	if currentVersion > MaxMigrationVersion {
		return fmt.Errorf("%w: database version %d, max supported %d", ErrSchemaTooNew, currentVersion, MaxMigrationVersion)
	}

	// If there are pending migrations, take a backup
	if currentVersion < MaxMigrationVersion {
		if err := Backup(ctx, db, backupDir, "pre-migrate"); err != nil {
			return fmt.Errorf("failed to create backup before migration: %w", err)
		}
	}

	// Set the embedded filesystem for goose
	goose.SetBaseFS(embedMigrations)

	// Run migrations
	if currentVersion < MaxMigrationVersion {
		if err := goose.UpContext(ctx, db, "internal/db/migrations"); err != nil {
			return fmt.Errorf("failed to apply migrations: %w", err)
		}
	}

	// Run integrity checks
	if err := integrityCheck(ctx, db); err != nil {
		return fmt.Errorf("integrity check failed after migration: %w", err)
	}

	return nil
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

	// Check foreign keys
	var fkErr string
	if err := db.QueryRowContext(ctx, "PRAGMA foreign_key_check").Scan(&fkErr); err != nil {
		return fmt.Errorf("failed to run foreign_key_check: %w", err)
	}
	if fkErr != "" {
		return fmt.Errorf("foreign key check failed: %s", fkErr)
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
