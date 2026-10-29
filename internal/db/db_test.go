package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestOpen(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "financeanalysis-db-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.sqlite")

	// Open database
	db, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	// Verify pragmas are set
	var fk int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("failed to check foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("expected foreign_keys=1, got %d", fk)
	}

	var journalMode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("failed to check journal_mode: %v", err)
	}
	if journalMode != "DELETE" {
		t.Errorf("expected journal_mode=DELETE, got %q", journalMode)
	}
}

func TestMigrate(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "financeanalysis-migrate-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.sqlite")
	backupDir := filepath.Join(tmpDir, "backups")

	// Create database
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	// Apply migrations
	if err := Migrate(context.Background(), db, backupDir); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	// Verify category table exists and has data
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM category").Scan(&count); err != nil {
		t.Fatalf("failed to count categories: %v", err)
	}
	if count != 4 {
		t.Errorf("expected 4 categories, got %d", count)
	}

	// Verify integrity
	if err := integrityCheck(context.Background(), db); err != nil {
		t.Fatalf("integrity check failed: %v", err)
	}
}

func TestMigrateUpToDate(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "financeanalysis-up-to-date-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.sqlite")
	backupDir := filepath.Join(tmpDir, "backups")

	// Create and migrate database
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	if err := Migrate(context.Background(), db, backupDir); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	// Count backups before second migration
	backupFiles, err := filepath.Glob(filepath.Join(backupDir, "*.sqlite"))
	if err != nil {
		t.Fatalf("failed to list backups: %v", err)
	}
	backupCount := len(backupFiles)

	// Run migration again - should be no-op, no new backup
	if err := Migrate(context.Background(), db, backupDir); err != nil {
		t.Fatalf("failed to migrate again: %v", err)
	}

	// Verify no new backup was created
	backupFiles, err = filepath.Glob(filepath.Join(backupDir, "*.sqlite"))
	if err != nil {
		t.Fatalf("failed to list backups: %v", err)
	}
	if len(backupFiles) != backupCount {
		t.Errorf("expected %d backups, got %d", backupCount, len(backupFiles))
	}
}

func TestBackupRetention(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "financeanalysis-backup-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	backupDir := filepath.Join(tmpDir, "backups")

	// Create some dummy backup files
	for i := 0; i < 25; i++ {
		backupPath := filepath.Join(backupDir, filepath.Base("backup-00000.sqlite"))
		// Use different timestamps so they have different modification times
		if err := os.WriteFile(backupPath, []byte{}, 0644); err != nil {
			t.Fatalf("failed to create backup file: %v", err)
		}
	}

	// Retain only 20
	if err := RetainBackups(backupDir, 20); err != nil {
		t.Fatalf("failed to retain backups: %v", err)
	}

	// Count remaining backups
	backupFiles, err := filepath.Glob(filepath.Join(backupDir, "*.sqlite"))
	if err != nil {
		t.Fatalf("failed to list backups: %v", err)
	}

	if len(backupFiles) > 20 {
		t.Errorf("expected at most 20 backups, got %d", len(backupFiles))
	}
}
