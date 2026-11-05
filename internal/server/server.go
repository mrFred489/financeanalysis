// Package server provides the HTTP server for the application.
package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mrFred489/financeanalysis/internal/db"
	"github.com/mrFred489/financeanalysis/internal/imports"
	"github.com/mrFred489/financeanalysis/internal/webui"
)

// ErrNotFound is returned when a resource is not found.
var ErrNotFound = errors.New("not found")

// maxUploadBytes is the body limit for file-upload routes (50 MiB).
const maxUploadBytes = 50 << 20

// maxDefaultBytes is the default body limit for all other routes (1 MiB).
const maxDefaultBytes = 1 << 20

// Server holds the HTTP server state.
type Server struct {
	addr          string
	dataDir       string
	dbPath        string
	backupDir     string
	previewSecret []byte

	mu sync.RWMutex
	db *sql.DB
}

// NewServer creates a new HTTP server serving the API and the embedded SPA.
func NewServer(addr, dataDir string) (*http.Server, error) {
	s := &Server{
		addr:      addr,
		dataDir:   dataDir,
		dbPath:    filepath.Join(dataDir, "finance.sqlite"),
		backupDir: filepath.Join(dataDir, "backups"),
	}
	secret, err := imports.NewSecret()
	if err != nil {
		return nil, err
	}
	s.previewSecret = secret

	// Reuse the global handle if main already opened the database.
	if d, err := db.GetDB(); err == nil {
		s.db = d
	}

	mux := http.NewServeMux()
	s.setupRoutes(mux)
	handler := s.applyMiddleware(mux)

	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}, nil
}

func (s *Server) setupRoutes(mux *http.ServeMux) {
	// Status and database lifecycle
	mux.Handle("GET /api/status", s.handleStatus())
	mux.Handle("POST /api/database", s.handleCreateDatabase())
	mux.Handle("POST /api/database/upload", s.handleDatabaseUpload())
	mux.Handle("GET /api/database/download", s.handleDatabaseDownload())
	mux.Handle("POST /api/database/replace", s.handleDatabaseReplace())
	mux.Handle("GET /api/backups", s.handleListBackups())
	mux.Handle("POST /api/backups/{name}/restore", s.handleRestoreBackup())

	// Accounts
	mux.Handle("GET /api/accounts", s.handleListAccounts())
	mux.Handle("POST /api/accounts", s.handleCreateAccount())
	mux.Handle("GET /api/accounts/{id}", s.handleGetAccount())
	mux.Handle("PUT /api/accounts/{id}", s.handleUpdateAccount())
	mux.Handle("DELETE /api/accounts/{id}", s.handleDeleteAccount())

	// Categories and subcategories
	mux.Handle("GET /api/categories", s.handleListCategories())
	mux.Handle("GET /api/subcategories", s.handleListSubcategories())
	mux.Handle("POST /api/subcategories", s.handleCreateSubcategory())
	mux.Handle("PUT /api/subcategories/{id}", s.handleUpdateSubcategory())
	mux.Handle("DELETE /api/subcategories/{id}", s.handleDeleteSubcategory())

	// Imports
	mux.Handle("POST /api/imports/preview", s.handleImportPreview())
	mux.Handle("POST /api/imports", s.handleImportCommit())
	mux.Handle("POST /api/imports/{id}/undo", s.handleImportUndo())
	mux.Handle("GET /api/imports", s.handleListImports())
	mux.Handle("GET /api/imports/{id}", s.handleGetImport())

	// Statement lines
	mux.Handle("GET /api/lines", s.handleListLines())
	mux.Handle("GET /api/lines/{id}", s.handleGetLine())

	// Settings
	mux.Handle("GET /api/settings", s.handleGetSettings())
	mux.Handle("PUT /api/settings", s.handleUpdateSettings())

	// Catch-all API routes
	mux.Handle("GET /api/", s.handleAPINotFound())
	mux.Handle("POST /api/", s.handleAPINotFound())
	mux.Handle("PUT /api/", s.handleAPINotFound())
	mux.Handle("DELETE /api/", s.handleAPINotFound())

	// SPA routes - serve static files or fallback to index.html
	mux.Handle("/", s.handleSPA())
}

func (s *Server) applyMiddleware(next http.Handler) http.Handler {
	chain := next
	chain = securityHeadersMiddleware(chain)
	chain = maxBytesMiddleware(chain)
	chain = loggingMiddleware(chain)
	return chain
}

// getDB returns the current database handle (nil when no database exists yet).
func (s *Server) getDB() *sql.DB {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.db
}

// setDB replaces the current database handle.
func (s *Server) setDB(d *sql.DB) {
	s.mu.Lock()
	s.db = d
	s.mu.Unlock()
}

// requireDB returns the database handle, writing a 503 error response when
// no database has been created yet.
func (s *Server) requireDB(w http.ResponseWriter, r *http.Request) *sql.DB {
	d := s.getDB()
	if d == nil || !dbReady(d) {
		writeError(w, http.StatusServiceUnavailable, APIError{
			Code:    "database_missing",
			Message: "No database yet. Create a new one or upload an existing one from the welcome page.",
		})
		return nil
	}
	return d
}

// dbReady reports whether the database has been migrated (category table exists).
func dbReady(d *sql.DB) bool {
	var count int
	return d.QueryRow("SELECT COUNT(*) FROM category").Scan(&count) == nil
}

func (s *Server) handleStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := &StatusResponse{
			Version:  version,
			Database: "missing",
		}

		if d := s.getDB(); d != nil && dbReady(d) {
			status.Database = "ready"
			if v, err := db.SchemaVersion(r.Context(), d); err == nil {
				status.SchemaVersion = &v
			}
			if accounts, err := listAccounts(r.Context(), d); err == nil {
				status.AccountsConfigured = len(accounts) > 0
			}
		}

		writeJSON(w, http.StatusOK, status)
	}
}

func (s *Server) handleAPINotFound() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, APIError{
			Code:    "not_found",
			Message: "API endpoint not found",
		})
	}
}

func (s *Server) handleSPA() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		// Clean the path to prevent directory traversal
		cleanPath := path
		if strings.Contains(cleanPath, "..") {
			writeError(w, http.StatusBadRequest, APIError{
				Code:    "bad_request",
				Message: "Invalid path",
			})
			return
		}

		// Try to serve from the embedded dist directory
		filePath := "dist/" + cleanPath
		if f, err := webui.Dist.Open(filePath); err == nil {
			defer f.Close()
			info, err := f.Stat()
			if err == nil && !info.IsDir() {
				content, err := readAll(f)
				if err != nil {
					writeError(w, http.StatusInternalServerError, APIError{Code: "read_error", Message: err.Error()})
					return
				}
				w.Header().Set("Content-Type", getContentType(cleanPath))
				if strings.HasPrefix(cleanPath, "_app/immutable/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "no-cache")
				}
				w.Write(content)
				return
			}
		}

		// Fallback to index.html for client-side routing
		if f, err := webui.Dist.Open("dist/index.html"); err == nil {
			defer f.Close()
			content, err := readAll(f)
			if err != nil {
				writeError(w, http.StatusInternalServerError, APIError{Code: "read_error", Message: err.Error()})
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			w.Write(content)
			return
		}

		// No frontend built (no embedui tag) - return a simple message
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `<html><body><h1>FinanceAnalysis</h1><p>Frontend not built yet. Run <code>task build:web</code> and build with <code>-tags embedui</code>.</p></body></html>`)
	}
}

func readAll(f fs.File) ([]byte, error) {
	return io.ReadAll(f)
}

// getContentType returns the appropriate content type for a file path.
func getContentType(path string) string {
	ext := ""
	if i := strings.LastIndex(path, "."); i >= 0 {
		ext = path[i:]
	}
	switch ext {
	case ".html":
		return "text/html; charset=utf-8"
	case ".js":
		return "application/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".svg":
		return "image/svg+xml; charset=utf-8"
	case ".woff2":
		return "font/woff2"
	default:
		return "application/octet-stream"
	}
}

// APIError represents an API error response.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ErrorResponse is the top-level error response.
type ErrorResponse struct {
	Error APIError `json:"error"`
}

// writeError writes an error response.
func writeError(w http.ResponseWriter, statusCode int, err APIError) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(ErrorResponse{Error: err})
}

// orEmpty returns s if non-nil, otherwise an empty slice, so list endpoints
// encode as [] instead of null (clients call .map on the result).
func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

// decodeJSON decodes a JSON request body; an empty body leaves v untouched.
func decodeJSON(r *http.Request, v interface{}) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	return json.NewDecoder(r.Body).Decode(v)
}

// StatusResponse is the response for the /api/status endpoint.
type StatusResponse struct {
	Version            string `json:"version"`
	Database           string `json:"database"` // "missing", "ready"
	SchemaVersion      *int64 `json:"schemaVersion,omitempty"`
	AccountsConfigured bool   `json:"accountsConfigured,omitempty"`
}

// Middleware for request logging
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Debug("request started", "method", r.Method, "path", r.URL.Path)
		wrapped := &responseWriter{ResponseWriter: w}
		next.ServeHTTP(wrapped, r)
		slog.Debug("request completed", "method", r.Method, "path", r.URL.Path, "status", wrapped.status)
	})
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(statusCode int) {
	rw.status = statusCode
	rw.ResponseWriter.WriteHeader(statusCode)
}

// Write implements io.Writer for responseWriter.
func (rw *responseWriter) Write(b []byte) (int, error) {
	if rw.status == 0 {
		rw.status = http.StatusOK
	}
	return rw.ResponseWriter.Write(b)
}

// maxBytesMiddleware applies a route-specific request body limit:
// 50 MiB for upload routes, 1 MiB for everything else.
func maxBytesMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := int64(maxDefaultBytes)
		switch {
		case r.URL.Path == "/api/imports/preview",
			r.URL.Path == "/api/database/upload",
			r.URL.Path == "/api/database/replace":
			limit = maxUploadBytes
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}

// Middleware for security headers
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'")
		next.ServeHTTP(w, r)
	})
}

// version is set at build time via ldflags
var version = "dev"
