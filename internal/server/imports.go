package server

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/mrFred489/financeanalysis/internal/db/dbq"
	"github.com/mrFred489/financeanalysis/internal/imports"
)

// parseAccountIDForm reads the "accountId" multipart form value.
func parseAccountIDForm(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.FormValue("accountId"), 10, 64)
}

// handleImportPreview implements IMP-5: parse + checks, nothing written.
func (s *Server) handleImportPreview() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_upload", Message: err.Error()})
			return
		}
		accountID, err := parseAccountIDForm(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "validation", Message: "accountId form field is required"})
			return
		}
		if _, err := dbq.New(d).GetAccount(r.Context(), accountID); err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_account", Message: "account not found"})
			return
		}
		fileName, content, err := readUploadedFile(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_upload", Message: err.Error()})
			return
		}

		res, err := imports.Preview(r.Context(), dbq.New(d), s.previewSecret, accountID, fileName, content)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, APIError{Code: "parse_failed", Message: err.Error()})
			return
		}
		res.ParseErrors = orEmpty(res.ParseErrors)
		res.Warnings = orEmpty(res.Warnings)
		writeJSON(w, http.StatusOK, res)
	}
}

// handleImportCommit implements the commit phase: it takes the uploaded file,
// the account and the preview token, re-validates everything and writes (IMP-10/12).
func (s *Server) handleImportCommit() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_upload", Message: err.Error()})
			return
		}
		accountID, err := parseAccountIDForm(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "validation", Message: "accountId form field is required"})
			return
		}
		token := r.FormValue("previewToken")
		if token == "" {
			writeError(w, http.StatusBadRequest, APIError{Code: "validation", Message: "previewToken form field is required"})
			return
		}
		fileName, content, err := readUploadedFile(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_upload", Message: err.Error()})
			return
		}

		res, err := imports.Commit(r.Context(), d, s.previewSecret, s.backupDir, accountID, fileName, content, token)
		if err != nil {
			if errors.Is(err, imports.ErrInvalidPreviewToken) {
				writeError(w, http.StatusBadRequest, APIError{Code: "invalid_preview_token", Message: "The preview does not match this file. Preview again and retry."})
				return
			}
			writeError(w, http.StatusInternalServerError, APIError{Code: "commit_failed", Message: err.Error()})
			return
		}
		res.ParseErrors = orEmpty(res.ParseErrors)
		writeJSON(w, http.StatusCreated, res)
	}
}

// handleImportUndo implements IMP-13: undo the latest non-undone import, which
// must be the one addressed by the route.
func (s *Server) handleImportUndo() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_id", Message: "invalid import id"})
			return
		}
		latest, err := dbq.New(d).GetLatestNonUndoneImport(r.Context())
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusConflict, APIError{Code: "nothing_to_undo", Message: "There is no import to undo."})
				return
			}
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		if latest.ID != id {
			writeError(w, http.StatusConflict, APIError{Code: "not_latest_import", Message: "Only the most recent import can be undone."})
			return
		}
		undoneID, err := imports.UndoLatest(r.Context(), d, s.backupDir)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "undo_failed", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "undone", "importId": undoneID})
	}
}

// handleListImports implements IMP-14: the import history.
func (s *Server) handleListImports() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		batches, err := dbq.New(d).ListImportBatches(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, orEmpty(batches))
	}
}

// handleGetImport returns one import batch with its actual line count.
func (s *Server) handleGetImport() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_id", Message: "invalid import id"})
			return
		}
		q := dbq.New(d)
		batch, err := q.GetImportBatch(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusNotFound, APIError{Code: "not_found", Message: "import not found"})
			return
		}
		n, err := q.CountLinesByImport(r.Context(), sql.NullInt64{Int64: id, Valid: true})
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"batch":           batch,
			"actualLineCount": n,
		})
	}
}
