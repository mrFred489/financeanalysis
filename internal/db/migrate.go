// Package db provides database migration functionality.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mrFred489/financeanalysis/internal/colorx"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// ErrSchemaTooNew is returned when the database schema version is newer than what the binary supports.
var ErrSchemaTooNew = fmt.Errorf("database schema version is newer than the binary supports")

// CurrentSchemaVersion is the current schema version supported by this binary.
const CurrentSchemaVersion = 2

// maxBackups is the number of backups kept in the backups directory.
const maxBackups = 20

type migration struct {
	version int64
	name    string
	up      string
}

var migrationFilePattern = regexp.MustCompile(`^(\d{5})_[a-z0-9_]+\.sql$`)

// loadMigrations reads the embedded migration files and returns their Up sections, sorted by version.
func loadMigrations() ([]migration, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("failed to read migrations: %w", err)
	}
	var migrations []migration
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		m := migrationFilePattern.FindStringSubmatch(entry.Name())
		if m == nil {
			continue
		}
		version, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid migration version in %q: %w", entry.Name(), err)
		}
		content, err := migrationsFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("failed to read migration %q: %w", entry.Name(), err)
		}
		migrations = append(migrations, migration{
			version: version,
			name:    entry.Name(),
			up:      extractUpSection(string(content)),
		})
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].version < migrations[j].version })
	return migrations, nil
}

// extractUpSection returns the SQL between the goose Up and Down annotations.
func extractUpSection(content string) string {
	up := content
	if idx := strings.Index(up, "-- +goose Up"); idx >= 0 {
		up = up[idx+len("-- +goose Up"):]
	}
	if idx := strings.Index(up, "-- +goose Down"); idx >= 0 {
		up = up[:idx]
	}
	return strings.TrimSpace(up)
}

// SchemaVersion returns the latest applied goose migration version (0 for a fresh database).
func SchemaVersion(ctx context.Context, db *sql.DB) (int64, error) {
	var version int64
	err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version").Scan(&version)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to query schema version: %w", err)
	}
	return version, nil
}

// Migrate runs all pending migrations on the database.
// If the database is at a higher version than the binary supports, it returns ErrSchemaTooNew.
// If there are pending migrations, it takes a backup first.
func Migrate(ctx context.Context, db *sql.DB, backupDir string) error {
	currentVersion, err := SchemaVersion(ctx, db)
	if err != nil {
		return err
	}

	// Check if schema is too new
	if currentVersion > CurrentSchemaVersion {
		return fmt.Errorf("%w: database version %d, max supported %d", ErrSchemaTooNew, currentVersion, CurrentSchemaVersion)
	}

	migrations, err := loadMigrations()
	if err != nil {
		return err
	}

	var pending []migration
	for _, m := range migrations {
		if m.version > currentVersion && m.version <= CurrentSchemaVersion {
			pending = append(pending, m)
		}
	}

	if len(pending) > 0 {
		// Take a backup before migrating
		if err := Backup(ctx, db, backupDir, "pre-migrate"); err != nil {
			return fmt.Errorf("failed to create backup before migration: %w", err)
		}
		for _, m := range pending {
			if err := applyMigration(ctx, db, m); err != nil {
				return fmt.Errorf("failed to apply migration %s: %w", m.name, err)
			}
		}
		if err := RetainBackups(backupDir, maxBackups); err != nil {
			return fmt.Errorf("failed to retain backups: %w", err)
		}
	}

	// Run integrity checks
	if err := integrityCheck(ctx, db); err != nil {
		return fmt.Errorf("integrity check failed after migration: %w", err)
	}

	return nil
}

// applyMigration applies a single migration in a transaction and records its version.
func applyMigration(ctx context.Context, db *sql.DB, m migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS goose_db_version (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			version_id INTEGER NOT NULL UNIQUE,
			is_applied BOOLEAN NOT NULL DEFAULT 1,
			tstamp TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now'))
		)
	`); err != nil {
		return fmt.Errorf("failed to create goose_db_version table: %w", err)
	}

	if _, err := tx.ExecContext(ctx, m.up); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, 1)`, m.version); err != nil {
		return fmt.Errorf("failed to record migration version: %w", err)
	}

	// Data backfill for M1: seed default subcategories as shades of each category's base hue (CAT-5).
	if m.version == 2 {
		if err := seedDefaultSubcategories(ctx, tx); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// defaultSubcategories lists the seeded subcategory names per category key.
var defaultSubcategories = map[string][]string{
	"income":   {"Salary", "Other income"},
	"fixed":    {"Housing", "Insurance", "Subscriptions"},
	"variable": {"Groceries", "Dining", "Transport", "Shopping", "Leisure"},
	"savings":  {"Savings transfer", "Investment"},
}

// seedDefaultSubcategories inserts the default subcategories for each category,
// with colors auto-generated as shades of the category base hue. Idempotent.
func seedDefaultSubcategories(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "SELECT id, key, color FROM category ORDER BY id")
	if err != nil {
		return fmt.Errorf("failed to list categories: %w", err)
	}
	defer rows.Close()

	type cat struct {
		id    int64
		key   string
		color string
	}
	var cats []cat
	for rows.Next() {
		var c cat
		if err := rows.Scan(&c.id, &c.key, &c.color); err != nil {
			return err
		}
		cats = append(cats, c)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, c := range cats {
		names := defaultSubcategories[c.key]
		if names == nil {
			continue
		}
		for i, name := range names {
			color, err := colorx.Shade(c.color, i, len(names))
			if err != nil {
				return fmt.Errorf("failed to generate color for %q: %w", name, err)
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO subcategory (category_id, name, color, sort_order) VALUES (?, ?, ?, ?)
				ON CONFLICT (category_id, name) DO NOTHING
			`, c.id, name, color, i); err != nil {
				return fmt.Errorf("failed to seed subcategory %q: %w", name, err)
			}
		}
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

	// Check foreign keys - this may return no rows, which is OK
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("failed to run foreign_key_check: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		return fmt.Errorf("foreign key check failed")
	}
	return rows.Err()
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

	// Create backup filename with UTC timestamp and reason
	timestamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	backupName := fmt.Sprintf("finance-%s-%s.sqlite", timestamp, reason)
	backupPath := filepath.Join(dir, backupName)
	for i := 1; ; i++ {
		if _, err := os.Stat(backupPath); os.IsNotExist(err) {
			break
		}
		backupName = fmt.Sprintf("finance-%s-%s-%d.sqlite", timestamp, reason, i)
		backupPath = filepath.Join(dir, backupName)
	}

	// Run VACUUM INTO to create the backup
	if _, err := db.ExecContext(ctx, fmt.Sprintf("VACUUM INTO '%s'", backupPath)); err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	return nil
}

// RetainBackups keeps only the newest maxKeep backups in the directory.
func RetainBackups(dir string, maxKeep int) error {
	// List all backup files
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to list backups: %w", err)
	}

	// Filter and sort by modification time (newest first)
	var backups []struct {
		path  string
		mtime int64
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".sqlite") {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			backups = append(backups, struct {
				path  string
				mtime int64
			}{path: filepath.Join(dir, entry.Name()), mtime: info.ModTime().Unix()})
		}
	}

	// Sort by modification time (newest first), name as tiebreak for same-second backups
	sort.Slice(backups, func(i, j int) bool {
		if backups[i].mtime != backups[j].mtime {
			return backups[i].mtime > backups[j].mtime
		}
		return filepath.Base(backups[i].path) > filepath.Base(backups[j].path)
	})

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
