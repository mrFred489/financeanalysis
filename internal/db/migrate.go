// Package db provides database migration functionality.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"
	"github.com/pressly/goose/v3/database/dialect"

	"github.com/mrFred489/financeanalysis/internal/colorx"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// ErrSchemaTooNew is returned when the database schema version is newer than what the binary supports.
var ErrSchemaTooNew = fmt.Errorf("database schema version is newer than the binary supports")

// maxBackups is the number of backups kept in the backups directory.
const maxBackups = 20

var migrationFilePattern = regexp.MustCompile(`^(\d{5})_[a-z0-9_]+\.sql$`)

// providerFor returns a goose provider that applies the embedded migrations to db.
//
// The *sql.DB is opened with modernc.org/sqlite (driver "sqlite", pure Go), so
// CGO stays disabled; the provider only needs the SQL dialect. We register a
// custom store (DialectCustom) because goose's built-in sqlite3 querier has no
// TableExists, which makes the provider try to re-create goose_db_version on
// databases that already have it — e.g. every database written by the previous
// hand-rolled runner. sqliteQuerier adds TableExists, so existing databases and
// backups are recognized and used unchanged.
func providerFor(db *sql.DB) (*goose.Provider, error) {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("failed to read migrations: %w", err)
	}
	store, err := database.NewStoreFromQuerier(goose.DefaultTablename, sqliteQuerier{})
	if err != nil {
		return nil, err
	}
	return goose.NewProvider(goose.DialectCustom, db, sub,
		goose.WithStore(store),
		goose.WithSlog(slog.Default()),
	)
}

// sqliteQuerier implements goose's database/dialect.Querier for SQLite, with a
// working TableExists. The statements match goose's built-in sqlite3 querier.
type sqliteQuerier struct{}

var _ dialect.Querier = sqliteQuerier{}

func (sqliteQuerier) CreateTable(tableName string) string {
	return fmt.Sprintf(`CREATE TABLE %s (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		version_id INTEGER NOT NULL,
		is_applied INTEGER NOT NULL,
		tstamp TIMESTAMP DEFAULT (datetime('now'))
	)`, tableName)
}

func (sqliteQuerier) InsertVersion(tableName string) string {
	return fmt.Sprintf(`INSERT INTO %s (version_id, is_applied) VALUES (?, ?)`, tableName)
}

func (sqliteQuerier) DeleteVersion(tableName string) string {
	return fmt.Sprintf(`DELETE FROM %s WHERE version_id=?`, tableName)
}

func (sqliteQuerier) GetMigrationByVersion(tableName string) string {
	return fmt.Sprintf(`SELECT tstamp, is_applied FROM %s WHERE version_id=? ORDER BY tstamp DESC LIMIT 1`, tableName)
}

func (sqliteQuerier) ListMigrations(tableName string) string {
	return fmt.Sprintf(`SELECT version_id, is_applied from %s ORDER BY id DESC`, tableName)
}

func (sqliteQuerier) GetLatestVersion(tableName string) string {
	return fmt.Sprintf(`SELECT MAX(version_id) FROM %s`, tableName)
}

// TableExists reports whether the version table exists. Unlike the built-in
// sqlite3 querier, this lets goose recognize pre-existing goose_db_version tables.
func (sqliteQuerier) TableExists(tableName string) string {
	return fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = '%s')`, tableName)
}

// MaxKnownVersion returns the highest migration version embedded in this binary.
func MaxKnownVersion() int64 {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		// The migrations directory is embedded at build time; failing to read it
		// is a build/packaging bug, not a runtime condition.
		panic(fmt.Sprintf("failed to read embedded migrations: %v", err))
	}
	var max int64
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
			panic(fmt.Sprintf("invalid migration version in %q: %v", entry.Name(), err))
		}
		if version > max {
			max = version
		}
	}
	return max
}

// SchemaVersion returns the latest applied goose migration version (0 for a fresh database).
func SchemaVersion(ctx context.Context, db *sql.DB) (int64, error) {
	p, err := providerFor(db)
	if err != nil {
		return 0, err
	}
	version, err := p.GetDBVersion(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to query schema version: %w", err)
	}
	return version, nil
}

// Migrate runs all pending migrations on the database via the goose provider.
// If the database is at a higher version than the binary supports, it returns ErrSchemaTooNew.
// If an existing database has pending migrations, it takes a backup first.
func Migrate(ctx context.Context, db *sql.DB, backupDir string) error {
	p, err := providerFor(db)
	if err != nil {
		return err
	}
	maxVersion := MaxKnownVersion()

	before, err := p.GetDBVersion(ctx)
	if err != nil {
		return fmt.Errorf("failed to query schema version: %w", err)
	}

	// Check if schema is too new
	if before > maxVersion {
		return fmt.Errorf("%w: database version %d, max supported %d", ErrSchemaTooNew, before, maxVersion)
	}

	// Back up an existing database before applying pending migrations. A fresh
	// database (version 0) is not backed up, and neither is an up-to-date one.
	backedUp := false
	if before > 0 && before < maxVersion {
		if err := Backup(ctx, db, backupDir, "pre-migrate"); err != nil {
			return fmt.Errorf("failed to create backup before migration: %w", err)
		}
		backedUp = true
	}

	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("failed to apply migrations: %w", err)
	}

	// Data backfill for M1: seed default subcategories the first time migration
	// 00002 is applied (fresh databases and upgrades crossing v2). Idempotent, so
	// running it in its own transaction instead of inside migration 00002 is safe,
	// and user-deleted defaults are never re-added on later upgrades.
	if before < 2 && maxVersion >= 2 {
		if err := seedDefaultSubcategories(ctx, db); err != nil {
			return fmt.Errorf("failed to seed default subcategories: %w", err)
		}
	}

	if backedUp {
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

// downTo rolls the database back to the given version. Dev-only: the production
// rollback strategy is backups (docs/migrations.md). Exposed for tests.
func downTo(ctx context.Context, db *sql.DB, version int64) error {
	p, err := providerFor(db)
	if err != nil {
		return err
	}
	_, err = p.DownTo(ctx, version)
	return err
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
// It runs in its own transaction.
func seedDefaultSubcategories(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

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
	return tx.Commit()
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
