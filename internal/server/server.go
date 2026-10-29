// Package server provides the HTTP server for the application.
package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mrFred489/financeanalysis/internal/db"
)

// ErrNotFound is returned when a resource is not found.
var ErrNotFound = errors.New("not found")

// Server holds the HTTP server state.
type Server struct {
	addr      string
	backupDir string
	db        *sql.DB
}

// NewServer creates a new HTTP server.
func NewServer(addr, backupDir string) (*http.Server, error) {
	srv := &Server{
		addr:      addr,
		backupDir: backupDir,
	}

	// Get database
	if dbConn, err := db.GetDB(); err == nil {
		srv.db = dbConn
	} else {
		slog.Warn("database not available", "error", err)
	}

	// Create router with middleware
	mux := http.NewServeMux()
	
	// Set up routes
	srv.setupRoutes(mux)

	// Wrap with middleware
	handler := srv.applyMiddleware(mux)

	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MiB
	}, nil
}

func (s *Server) setupRoutes(mux *http.ServeMux) {
	// API routes
	mux.Handle("GET /api/status", s.handleStatus())
	mux.Handle("POST /api/database", s.handleCreateDatabase())

	// Catch-all API routes
	mux.Handle("GET /api/*", s.handleAPINotFound())
	mux.Handle("POST /api/*", s.handleAPINotFound())
	mux.Handle("PUT /api/*", s.handleAPINotFound())
	mux.Handle("DELETE /api/*", s.handleAPINotFound())

	// SPA routes - serve static files or fallback to index.html
	mux.Handle("/", s.handleSPA())
}

func (s *Server) applyMiddleware(next http.Handler) http.Handler {
	chain := next
	chain = securityHeadersMiddleware(chain)
	chain = maxBytesMiddleware(1<<20, chain) // 1 MiB
	chain = loggingMiddleware(chain)
	return chain
}

func (s *Server) handleStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := &StatusResponse{
			Version: version,
			Database: "missing",
		}

		if s.db != nil {
			// Check if database is ready
			var count int
			if err := s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM category").Scan(&count); err == nil {
				status.Database = "ready"
				// Get schema version
				var schemaVersion int64
				if err := s.db.QueryRowContext(r.Context(), "PRAGMA schema_version").Scan(&schemaVersion); err == nil {
					status.SchemaVersion = &schemaVersion
				}
			}
		}

		writeJSON(w, http.StatusOK, status)
	}
}

func (s *Server) handleCreateDatabase() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check if database already exists
		if s.db != nil {
			var count int
			if err := s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM category").Scan(&count); err == nil && count > 0 {
				writeError(w, http.StatusConflict, APIError{
					Code:    "database_exists",
					Message: "Database already exists",
				})
				return
			}
		}

		// Create and migrate database
		ctx := r.Context()
		dbPath := db.GetDBPath()

		// Open database (should already exist from main.go)
		if s.db == nil {
			var err error
			s.db, err = db.Open(ctx, dbPath)
			if err != nil {
				writeError(w, http.StatusInternalServerError, APIError{
					Code:    "database_open_failed",
					Message: err.Error(),
				})
				return
			}
		}

		// Run migrations
		if err := db.Migrate(ctx, s.db, s.backupDir); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{
				Code:    "migration_failed",
				Message: err.Error(),
			})
			return
		}

		// Return success
		writeJSON(w, http.StatusCreated, map[string]string{"status": "created"})
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
		// Try to serve static files first
		path := r.URL.Path
		if path == "/" {
			path = "index.html"
		}

		// Clean the path to prevent directory traversal
		cleanPath := filepath.Clean(path)
		if strings.HasPrefix(cleanPath, "..") {
			writeError(w, http.StatusBadRequest, APIError{
				Code:    "bad_request",
				Message: "Invalid path",
			})
			return
		}

		// Try to serve from dist directory
		distPath := filepath.Join("internal", "webui", "dist", cleanPath)
		if _, err := os.Stat(distPath); err == nil {
			// File exists, serve it
			content, err := os.ReadFile(distPath)
			if err != nil {
				writeError(w, http.StatusInternalServerError, APIError{
					Code:    "read_error",
					Message: err.Error(),
				})
				return
			}

			// Determine content type
			contentType := getContentType(cleanPath)
			w.Header().Set("Content-Type", contentType)

			// Set cache headers
			if strings.HasPrefix(cleanPath, "_app/immutable/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}

			w.Write(content)
			return
		}

		// Fallback to index.html for client-side routing
		indexPath := filepath.Join("internal", "webui", "dist", "index.html")
		if _, err := os.Stat(indexPath); err == nil {
			content, err := os.ReadFile(indexPath)
			if err != nil {
				writeError(w, http.StatusInternalServerError, APIError{
					Code:    "read_error",
					Message: err.Error(),
				})
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(content)
			return
		}

		// No frontend built yet - return a simple message
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `<html><body><h1>FinanceAnalysis</h1><p>Frontend not built yet. Run <code>task build:web</code>.</p></body></html>`)
	}
}

// getContentType returns the appropriate content type for a file path.
func getContentType(path string) string {
	ext := filepath.Ext(path)
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

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

// StatusResponse is the response for the /api/status endpoint.
type StatusResponse struct {
	Version       string  `json:"version"`
	Database      string  `json:"database"` // "missing", "ready"
	SchemaVersion *int64 `json:"schemaVersion,omitempty"`
}

// Middleware for request logging and recovery
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Log the request
		slog.Debug("request started", "method", r.Method, "path", r.URL.Path)

		// Wrap the response writer to capture status code
		wrapped := &responseWriter{ResponseWriter: w}

		// Call the next handler
		next.ServeHTTP(wrapped, r)

		// Log the response
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

// Middleware for limiting request body size
func maxBytesMiddleware(maxBytes int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		next.ServeHTTP(w, r)
	})
}

// Middleware for security headers
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set security headers
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'")
		next.ServeHTTP(w, r)
	})
}

// version is set at build time via ldflags
var version = "dev"
