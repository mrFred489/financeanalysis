package server

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"

	"github.com/mrFred489/financeanalysis/internal/db/dbq"
)

func listAccounts(ctx context.Context, d *sql.DB) ([]*dbq.Account, error) {
	return dbq.New(d).ListAccounts(ctx)
}

// accountRequest is the body for POST/PUT /api/accounts. Nil fields are left unchanged on update.
type accountRequest struct {
	Name          *string `json:"name"`
	AccountNumber *string `json:"account_number"`
	SortOrder     *int64  `json:"sort_order"`
	Archived      *bool   `json:"archived"`
}

func (s *Server) handleListAccounts() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		accounts, err := dbq.New(d).ListAccounts(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, orEmpty(accounts))
	}
}

func (s *Server) handleCreateAccount() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		var req accountRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_json", Message: err.Error()})
			return
		}
		if req.Name == nil || *req.Name == "" {
			writeError(w, http.StatusBadRequest, APIError{Code: "validation", Message: "name is required"})
			return
		}
		sortOrder := int64(0)
		if req.SortOrder != nil {
			sortOrder = *req.SortOrder
		}
		acc, err := dbq.New(d).CreateAccount(r.Context(), dbq.CreateAccountParams{
			Name:          *req.Name,
			AccountNumber: nullString(req.AccountNumber),
			SortOrder:     sortOrder,
		})
		if err != nil {
			writeError(w, http.StatusConflict, APIError{Code: "create_failed", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, acc)
	}
}

func (s *Server) handleGetAccount() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_id", Message: "invalid account id"})
			return
		}
		acc, err := dbq.New(d).GetAccount(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusNotFound, APIError{Code: "not_found", Message: "account not found"})
			return
		}
		writeJSON(w, http.StatusOK, acc)
	}
}

func (s *Server) handleUpdateAccount() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_id", Message: "invalid account id"})
			return
		}
		var req accountRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_json", Message: err.Error()})
			return
		}
		acc, err := dbq.New(d).UpdateAccount(r.Context(), dbq.UpdateAccountParams{
			Name:          nullString(req.Name),
			AccountNumber: nullString(req.AccountNumber),
			SortOrder:     nullInt64(req.SortOrder),
			Archived:      nullBoolAsInt(req.Archived),
			ID:            id,
		})
		if err != nil {
			writeError(w, http.StatusNotFound, APIError{Code: "not_found", Message: "account not found or update failed: " + err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, acc)
	}
}

func (s *Server) handleDeleteAccount() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_id", Message: "invalid account id"})
			return
		}
		q := dbq.New(d)
		// SETUP-6: an account can only be deleted if it has no lines
		n, err := q.CountAccountLines(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		if n > 0 {
			writeError(w, http.StatusConflict, APIError{Code: "account_has_lines", Message: "Account has statement lines and cannot be deleted. Archive it instead."})
			return
		}
		if err := q.DeleteAccount(r.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

func nullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

func nullInt64(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}

func nullBoolAsInt(b *bool) sql.NullInt64 {
	if b == nil {
		return sql.NullInt64{}
	}
	if *b {
		return sql.NullInt64{Int64: 1, Valid: true}
	}
	return sql.NullInt64{Int64: 0, Valid: true}
}
