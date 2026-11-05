package server

import (
	"net/http"

	"github.com/mrFred489/financeanalysis/internal/db/dbq"
)

// handleGetSettings returns all settings as a key/value map.
func (s *Server) handleGetSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		settings, err := dbq.New(d).ListSettings(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		result := map[string]string{}
		for _, kv := range settings {
			result[kv.Key] = kv.Value
		}
		writeJSON(w, http.StatusOK, map[string]any{"settings": result})
	}
}

// handleUpdateSettings upserts settings from a JSON object of key/value strings.
func (s *Server) handleUpdateSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		var body map[string]string
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_json", Message: err.Error()})
			return
		}
		tx, err := d.BeginTx(r.Context(), nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		defer tx.Rollback()
		q := dbq.New(d).WithTx(tx)
		for key, value := range body {
			if err := q.UpsertSetting(r.Context(), dbq.UpsertSettingParams{Key: key, Value: value}); err != nil {
				writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
				return
			}
		}
		if err := tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
	}
}
