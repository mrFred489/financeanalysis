package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mrFred489/financeanalysis/internal/colorx"
	"github.com/mrFred489/financeanalysis/internal/db"
	"github.com/mrFred489/financeanalysis/internal/db/dbq"
)

// createDatabaseRequest is the optional JSON body of POST /api/database,
// sent by the welcome wizard with the chosen accounts and subcategories (SETUP-3).
type createDatabaseRequest struct {
	Accounts []struct {
		Name          string  `json:"name"`
		AccountNumber *string `json:"account_number"`
	} `json:"accounts"`
	Subcategories []struct {
		CategoryID  *int64  `json:"category_id"`
		CategoryKey string  `json:"category_key"`
		Name        string  `json:"name"`
		Color       string  `json:"color"`
		Hotkey      *string `json:"hotkey"`
		SortOrder   int64   `json:"sort_order"`
	} `json:"subcategories"`
}

// handleCreateDatabase creates and migrates a new database (201), optionally
// seeding it with the accounts and subcategories chosen in the welcome wizard.
// Returns 409 when a database already exists.
func (s *Server) handleCreateDatabase() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d := s.getDB(); d != nil && dbReady(d) {
			writeError(w, http.StatusConflict, APIError{Code: "database_exists", Message: "Database already exists"})
			return
		}

		var req createDatabaseRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_json", Message: err.Error()})
			return
		}

		ctx := r.Context()
		d := s.getDB()
		if d == nil {
			var err error
			d, err = db.Open(ctx, s.dbPath)
			if err != nil {
				writeError(w, http.StatusInternalServerError, APIError{Code: "database_open_failed", Message: err.Error()})
				return
			}
		}
		// Make the global handle point at this database so Backup works during migration
		db.SwapDB(d, s.dbPath)

		if err := db.Migrate(ctx, d, s.backupDir); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "migration_failed", Message: err.Error()})
			return
		}

		// Seed accounts and (optionally) replace the default subcategories
		if len(req.Accounts) > 0 || len(req.Subcategories) > 0 {
			if err := seedFromWizard(ctx, d, &req); err != nil {
				writeError(w, http.StatusInternalServerError, APIError{Code: "seed_failed", Message: err.Error()})
				return
			}
		}

		db.SwapDB(d, s.dbPath)
		s.setDB(d)
		writeJSON(w, http.StatusCreated, map[string]string{"status": "created"})
	}
}

// seedFromWizard inserts the wizard's accounts and, when subcategories are given,
// replaces the seeded defaults (no lines exist yet, so this is safe).
func seedFromWizard(ctx context.Context, d *sql.DB, req *createDatabaseRequest) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := dbq.New(d).WithTx(tx)

	if len(req.Subcategories) > 0 {
		categories, err := q.ListCategories(ctx)
		if err != nil {
			return err
		}
		byKey := map[string]*dbq.Category{}
		for _, c := range categories {
			byKey[c.Key] = c
		}
		// No lines can exist yet, so the defaults can be replaced wholesale.
		if _, err := tx.ExecContext(ctx, "DELETE FROM subcategory"); err != nil {
			return err
		}
		perCategory := map[int64]int{}
		for _, sc := range req.Subcategories {
			var cat *dbq.Category
			if sc.CategoryID != nil {
				for _, c := range categories {
					if c.ID == *sc.CategoryID {
						cat = c
					}
				}
			} else if byKey[sc.CategoryKey] != nil {
				cat = byKey[sc.CategoryKey]
			}
			if cat == nil {
				return fmt.Errorf("unknown category for subcategory %q", sc.Name)
			}
			color := sc.Color
			if color == "" {
				i := perCategory[cat.ID]
				perCategory[cat.ID]++
				n := i + 1
				// upper bound for shade generation: count of wizard subs in this category
				total := 0
				for _, other := range req.Subcategories {
					oc := byKey[other.CategoryKey]
					if other.CategoryID != nil {
						for _, c := range categories {
							if c.ID == *other.CategoryID {
								oc = c
							}
						}
					}
					if oc != nil && oc.ID == cat.ID {
						total++
					}
				}
				color, err = colorx.Shade(cat.Color, i, total)
				if err != nil {
					return err
				}
				_ = n
			}
			if _, err := q.CreateSubcategory(ctx, dbq.CreateSubcategoryParams{
				CategoryID: cat.ID,
				Name:       sc.Name,
				Color:      color,
				Hotkey:     nullString(sc.Hotkey),
				SortOrder:  sc.SortOrder,
			}); err != nil {
				return err
			}
		}
	}

	for i, acc := range req.Accounts {
		if acc.Name == "" {
			return fmt.Errorf("account name is required")
		}
		if _, err := q.CreateAccount(ctx, dbq.CreateAccountParams{
			Name:          acc.Name,
			AccountNumber: nullString(acc.AccountNumber),
			SortOrder:     int64(i),
		}); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// validateDatabaseFile checks that an uploaded file is a SQLite database with
// this app's schema: older schemas are migrated later, newer ones are refused (SETUP-4).
func validateDatabaseFile(path string) error {
	d, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("not a SQLite database: %w", err)
	}
	defer d.Close()
	d.SetMaxOpenConns(1)

	ctx := context.Background()
	var count int
	if err := d.QueryRowContext(ctx, "SELECT COUNT(*) FROM category").Scan(&count); err != nil {
		return fmt.Errorf("not a FinanceAnalysis database (no category table): %w", err)
	}
	version, err := db.SchemaVersion(ctx, d)
	if err != nil {
		return fmt.Errorf("failed to read schema version: %w", err)
	}
	if version > db.CurrentSchemaVersion {
		return fmt.Errorf("%w: file is version %d, this app supports up to %d", db.ErrSchemaTooNew, version, db.CurrentSchemaVersion)
	}
	return nil
}

// readUploadedFile reads the multipart "file" field into memory (bounded by the 50 MiB body limit).
func readUploadedFile(r *http.Request) (string, []byte, error) {
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		return "", nil, err
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return "", nil, fmt.Errorf("missing file field: %w", err)
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil {
		return "", nil, err
	}
	return header.Filename, content, nil
}

// writeDatabaseFile atomically replaces the database file with the given content.
func writeDatabaseFile(dbPath string, content []byte) error {
	tmp := dbPath + ".tmp"
	if err := os.WriteFile(tmp, content, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, dbPath)
}

// openAndMigrate opens the database at s.dbPath, migrates it, and makes it the active handle.
func (s *Server) openAndMigrate(ctx context.Context) (*sql.DB, error) {
	d, err := db.Open(ctx, s.dbPath)
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(ctx, d, s.backupDir); err != nil {
		d.Close()
		return nil, err
	}
	db.SwapDB(d, s.dbPath)
	s.setDB(d)
	return d, nil
}

// handleDatabaseUpload implements SETUP-4 for the welcome page: upload an existing
// database file. Older schemas are backed up and migrated; newer ones are refused.
func (s *Server) handleDatabaseUpload() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d := s.getDB(); d != nil && dbReady(d) {
			writeError(w, http.StatusConflict, APIError{Code: "database_exists", Message: "Database already exists. Use replace instead."})
			return
		}
		fileName, content, err := readUploadedFile(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_upload", Message: err.Error()})
			return
		}

		// Validate on a temp copy before touching the real database file
		tmpDir, err := os.MkdirTemp("", "fa-upload-*")
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "io_error", Message: err.Error()})
			return
		}
		defer os.RemoveAll(tmpDir)
		tmpPath := filepath.Join(tmpDir, "upload.sqlite")
		if err := os.WriteFile(tmpPath, content, 0644); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "io_error", Message: err.Error()})
			return
		}
		if err := validateDatabaseFile(tmpPath); err != nil {
			status := http.StatusUnprocessableEntity
			code := "invalid_database"
			if errors.Is(err, db.ErrSchemaTooNew) {
				code = "schema_too_new"
			}
			writeError(w, status, APIError{Code: code, Message: fmt.Sprintf("Cannot use %q: %v", fileName, err)})
			return
		}

		ctx := r.Context()
		if err := writeDatabaseFile(s.dbPath, content); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "io_error", Message: err.Error()})
			return
		}
		if _, err := s.openAndMigrate(ctx); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "migration_failed", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"status": "uploaded"})
	}
}

// handleDatabaseDownload streams a consistent snapshot of the database (SETUP-5).
func (s *Server) handleDatabaseDownload() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		tmpDir, err := os.MkdirTemp("", "fa-download-*")
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "io_error", Message: err.Error()})
			return
		}
		defer os.RemoveAll(tmpDir)
		tmpPath := filepath.Join(tmpDir, "finance.sqlite")
		if _, err := d.ExecContext(r.Context(), fmt.Sprintf("VACUUM INTO '%s'", tmpPath)); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "backup_failed", Message: err.Error()})
			return
		}
		downloadName := fmt.Sprintf("finance-%s.sqlite", time.Now().UTC().Format("20060102T150405Z"))
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", downloadName))
		http.ServeFile(w, r, tmpPath)
	}
}

// handleDatabaseReplace replaces the current database with an uploaded file,
// taking an automatic backup of the current one first (SETUP-5).
func (s *Server) handleDatabaseReplace() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		fileName, content, err := readUploadedFile(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_upload", Message: err.Error()})
			return
		}

		tmpDir, err := os.MkdirTemp("", "fa-replace-*")
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "io_error", Message: err.Error()})
			return
		}
		defer os.RemoveAll(tmpDir)
		tmpPath := filepath.Join(tmpDir, "replace.sqlite")
		if err := os.WriteFile(tmpPath, content, 0644); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "io_error", Message: err.Error()})
			return
		}
		if err := validateDatabaseFile(tmpPath); err != nil {
			status := http.StatusUnprocessableEntity
			code := "invalid_database"
			if errors.Is(err, db.ErrSchemaTooNew) {
				code = "schema_too_new"
			}
			writeError(w, status, APIError{Code: code, Message: fmt.Sprintf("Cannot use %q: %v", fileName, err)})
			return
		}

		ctx := r.Context()
		// Automatic backup of the current database before replacing it
		if err := db.Backup(ctx, d, s.backupDir, "pre-replace"); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "backup_failed", Message: err.Error()})
			return
		}
		if err := writeDatabaseFile(s.dbPath, content); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "io_error", Message: err.Error()})
			return
		}
		if _, err := s.openAndMigrate(ctx); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "migration_failed", Message: err.Error()})
			return
		}
		if err := db.RetainBackups(s.backupDir, 20); err != nil {
			slog.Warn("backup retention failed", "error", err)
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "replaced"})
	}
}

// backupInfo is one entry of GET /api/backups.
type backupInfo struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"`
}

func (s *Server) handleListBackups() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.requireDB(w, r)
		entries, err := os.ReadDir(s.backupDir)
		if err != nil {
			if os.IsNotExist(err) {
				writeJSON(w, http.StatusOK, []backupInfo{})
				return
			}
			writeError(w, http.StatusInternalServerError, APIError{Code: "io_error", Message: err.Error()})
			return
		}
		var backups []backupInfo
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sqlite") {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			backups = append(backups, backupInfo{
				Name:    entry.Name(),
				Size:    info.Size(),
				ModTime: info.ModTime().UTC().Format(time.RFC3339),
			})
		}
		writeJSON(w, http.StatusOK, orEmpty(backups))
	}
}

// handleRestoreBackup restores a backup by name, taking a backup of the current
// database first (SETUP-5).
func (s *Server) handleRestoreBackup() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		name := r.PathValue("name")
		if filepath.Base(name) != name || !strings.HasSuffix(name, ".sqlite") {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_name", Message: "invalid backup name"})
			return
		}
		backupPath := filepath.Join(s.backupDir, name)
		content, err := os.ReadFile(backupPath)
		if err != nil {
			writeError(w, http.StatusNotFound, APIError{Code: "not_found", Message: "backup not found"})
			return
		}

		ctx := r.Context()
		if err := db.Backup(ctx, d, s.backupDir, "pre-restore"); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "backup_failed", Message: err.Error()})
			return
		}
		if err := writeDatabaseFile(s.dbPath, content); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "io_error", Message: err.Error()})
			return
		}
		if _, err := s.openAndMigrate(ctx); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "migration_failed", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "restored", "backup": name})
	}
}
