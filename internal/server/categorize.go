package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/mrFred489/financeanalysis/internal/actions"
	"github.com/mrFred489/financeanalysis/internal/db/dbq"
)

// M2 categorization mutation endpoints + the undo/redo API (plan step 3, D3).
// Each mutation endpoint: validate -> begin tx -> snapshot before -> mutate ->
// snapshot after -> actions.Record -> commit -> return the affected line(s).

type categorizeBody struct {
	SubcategoryID *int64 `json:"subcategoryId"`
}

type bulkCategorizeBody struct {
	LineIDs       []int64 `json:"lineIds"`
	SubcategoryID *int64  `json:"subcategoryId"`
}

type spreadBody struct {
	Months *int64 `json:"months"`
}

// actionChangeJSON is the API shape of one stored row snapshot.
type actionChangeJSON struct {
	Entity     string `json:"entity"`
	EntityID   int64  `json:"entityId"`
	BeforeJson string `json:"beforeJson"`
	AfterJson  string `json:"afterJson"`
}

// actionJSON is the API shape of one undoable action.
type actionJSON struct {
	ID          int64              `json:"id"`
	Kind        string             `json:"kind"`
	Description string             `json:"description"`
	CreatedAt   string             `json:"createdAt"`
	UndoneAt    *string            `json:"undoneAt"`
	ChangeCount int64              `json:"changeCount"`
	Changes     []actionChangeJSON `json:"changes,omitempty"`
}

func actionToJSON(a *actions.Action) actionJSON {
	out := actionJSON{
		ID: a.ID, Kind: a.Kind, Description: a.Description,
		CreatedAt: a.CreatedAt, ChangeCount: a.ChangeCount,
	}
	if a.UndoneAt.Valid {
		out.UndoneAt = &a.UndoneAt.String
	}
	for _, c := range a.Changes {
		out.Changes = append(out.Changes, actionChangeJSON{
			Entity: c.Entity, EntityID: c.EntityID, BeforeJson: c.BeforeJson, AfterJson: c.AfterJson,
		})
	}
	return out
}

func lineJSONFromRow(row *dbq.GetStatementLineRow) lineJSON {
	return lineJSON{
		ID: row.ID, AccountID: row.AccountID, AccountName: row.AccountName,
		ImportID:            nullIntPtr(row.ImportID),
		BookingDate:         row.BookingDate,
		TransactionDate:     nullStrPtr(row.TransactionDate),
		ValueDate:           nullStrPtr(row.ValueDate),
		Message:             row.Message,
		AmountMinor:         row.AmountMinor,
		BalanceMinor:        nullIntPtr(row.BalanceMinor),
		Currency:            row.Currency,
		BankRef:             nullStrPtr(row.BankRef),
		CounterpartyAccount: nullStrPtr(row.CounterpartyAccount),
		FileRow:             row.FileRow, Seq: row.Seq, Occurrence: row.Occurrence,
		SubcategoryID:    nullIntPtr(row.SubcategoryID),
		SubcategoryName:  nullStrPtr(row.SubcategoryName),
		SubcategoryColor: nullStrPtr(row.SubcategoryColor),
		CategoryID:       nullIntPtr(row.CategoryID),
		CategoryName:     nullStrPtr(row.CategoryName),
		Source:           row.Source, RuleID: nullIntPtr(row.RuleID), SpreadMonths: row.SpreadMonths,
	}
}

// getSubcategoryOrError validates the subcategory id, writing the error response.
func getSubcategoryOrError(w http.ResponseWriter, q *dbq.Queries, ctx context.Context, id int64) *dbq.Subcategory {
	sub, err := q.GetSubcategory(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, APIError{Code: "not_found", Message: "subcategory not found"})
		return nil
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
		return nil
	}
	return sub
}

// handleCategorizeLine: POST /api/lines/{id}/categorize (CATZ-4).
func (s *Server) handleCategorizeLine() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		ctx := r.Context()
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_id", Message: "invalid line id"})
			return
		}
		var body categorizeBody
		if err := decodeJSON(r, &body); err != nil || body.SubcategoryID == nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_params", Message: "body must be {\"subcategoryId\": number}"})
			return
		}
		sub := getSubcategoryOrError(w, dbq.New(d), ctx, *body.SubcategoryID)
		if sub == nil {
			return
		}

		tx, err := d.BeginTx(ctx, nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		defer tx.Rollback()
		q := dbq.New(tx)

		before, err := q.GetLineForAction(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, APIError{Code: "not_found", Message: "line not found"})
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		if _, err := q.SetLineSubcategory(ctx, dbq.SetLineSubcategoryParams{
			SubcategoryID: sql.NullInt64{Int64: *body.SubcategoryID, Valid: true}, ID: id,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		after, err := q.GetLineForAction(ctx, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		desc := fmt.Sprintf("Categorized line #%d as %s", id, sub.Name)
		if _, err := actions.Record(ctx, tx, "categorize", desc, []actions.Change{
			{Entity: actions.EntityStatementLine, EntityID: id, Before: *before, After: *after},
		}); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		if err := tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}

		row, err := dbq.New(d).GetStatementLine(ctx, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, lineJSONFromRow(row))
	}
}

// handleBulkCategorize: POST /api/lines/categorize (CATZ-11). One undoable action.
func (s *Server) handleBulkCategorize() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		ctx := r.Context()
		var body bulkCategorizeBody
		if err := decodeJSON(r, &body); err != nil || body.SubcategoryID == nil || len(body.LineIDs) == 0 {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_params", Message: "body must be {\"lineIds\": [...], \"subcategoryId\": number}"})
			return
		}
		if len(body.LineIDs) > 1000 {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_params", Message: "too many lineIds (max 1000)"})
			return
		}
		sub := getSubcategoryOrError(w, dbq.New(d), ctx, *body.SubcategoryID)
		if sub == nil {
			return
		}

		tx, err := d.BeginTx(ctx, nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		defer tx.Rollback()
		q := dbq.New(tx)

		var changes []actions.Change
		for _, id := range body.LineIDs {
			before, err := q.GetLineForAction(ctx, id)
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, APIError{Code: "not_found", Message: fmt.Sprintf("line %d not found", id)})
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
				return
			}
			if _, err := q.SetLineSubcategory(ctx, dbq.SetLineSubcategoryParams{
				SubcategoryID: sql.NullInt64{Int64: *body.SubcategoryID, Valid: true}, ID: id,
			}); err != nil {
				writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
				return
			}
			after, err := q.GetLineForAction(ctx, id)
			if err != nil {
				writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
				return
			}
			changes = append(changes, actions.Change{Entity: actions.EntityStatementLine, EntityID: id, Before: *before, After: *after})
		}
		desc := fmt.Sprintf("Categorized %d lines as %s", len(body.LineIDs), sub.Name)
		if _, err := actions.Record(ctx, tx, "categorize_bulk", desc, changes); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		if err := tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}

		lines := []lineJSON{}
		for _, id := range body.LineIDs {
			row, err := dbq.New(d).GetStatementLine(ctx, id)
			if err != nil {
				writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
				return
			}
			lines = append(lines, lineJSONFromRow(row))
		}
		writeJSON(w, http.StatusOK, map[string]any{"lines": lines})
	}
}

// handleSetSpread: POST /api/lines/{id}/spread (VIZ-7, CATZ-7). months 1..36.
func (s *Server) handleSetSpread() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		ctx := r.Context()
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_id", Message: "invalid line id"})
			return
		}
		var body spreadBody
		if err := decodeJSON(r, &body); err != nil || body.Months == nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_params", Message: "body must be {\"months\": number}"})
			return
		}
		if *body.Months < 1 || *body.Months > 36 {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_params", Message: "months must be between 1 and 36"})
			return
		}

		tx, err := d.BeginTx(ctx, nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		defer tx.Rollback()
		q := dbq.New(tx)

		before, err := q.GetLineForAction(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, APIError{Code: "not_found", Message: "line not found"})
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		if _, err := q.SetLineSpread(ctx, dbq.SetLineSpreadParams{SpreadMonths: *body.Months, ID: id}); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		after, err := q.GetLineForAction(ctx, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		desc := fmt.Sprintf("Set spread of line #%d to %d months", id, *body.Months)
		if _, err := actions.Record(ctx, tx, "spread", desc, []actions.Change{
			{Entity: actions.EntityStatementLine, EntityID: id, Before: *before, After: *after},
		}); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		if err := tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}

		row, err := dbq.New(d).GetStatementLine(ctx, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, lineJSONFromRow(row))
	}
}

// handleResetLine: POST /api/lines/{id}/reset (D4: clears the manual assignment).
func (s *Server) handleResetLine() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		ctx := r.Context()
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_id", Message: "invalid line id"})
			return
		}

		tx, err := d.BeginTx(ctx, nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		defer tx.Rollback()
		q := dbq.New(tx)

		before, err := q.GetLineForAction(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, APIError{Code: "not_found", Message: "line not found"})
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		if _, err := q.ResetLineToRules(ctx, id); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		after, err := q.GetLineForAction(ctx, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		desc := fmt.Sprintf("Reset line #%d to rules", id)
		if _, err := actions.Record(ctx, tx, "reset", desc, []actions.Change{
			{Entity: actions.EntityStatementLine, EntityID: id, Before: *before, After: *after},
		}); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		if err := tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}

		row, err := dbq.New(d).GetStatementLine(ctx, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, lineJSONFromRow(row))
	}
}

// handleListActions: GET /api/actions (UNDO-2/UNDO-5, feeds the M6 panel).
func (s *Server) handleListActions() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		limit := 50
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		if limit > 200 {
			limit = 200
		}
		list, err := actions.List(r.Context(), d, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		out := orEmpty(list)
		jsonList := make([]actionJSON, 0, len(out))
		for i := range out {
			jsonList = append(jsonList, actionToJSON(&out[i]))
		}
		writeJSON(w, http.StatusOK, jsonList)
	}
}

// handleUndo: POST /api/actions/undo (UNDO-3). 409 when nothing to undo.
func (s *Server) handleUndo() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		action, err := actions.Undo(r.Context(), d)
		if errors.Is(err, actions.ErrNothingToUndo) {
			writeError(w, http.StatusConflict, APIError{Code: "nothing_to_undo", Message: "Nothing to undo"})
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, actionToJSON(action))
	}
}

// handleRedo: POST /api/actions/redo. 409 when nothing to redo.
func (s *Server) handleRedo() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		action, err := actions.Redo(r.Context(), d)
		if errors.Is(err, actions.ErrNothingToRedo) {
			writeError(w, http.StatusConflict, APIError{Code: "nothing_to_redo", Message: "Nothing to redo"})
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, actionToJSON(action))
	}
}
