package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/mrFred489/financeanalysis/internal/actions"
	"github.com/mrFred489/financeanalysis/internal/db/dbq"
	"github.com/mrFred489/financeanalysis/internal/rules"
)

// M3 rules API (plan docs/plans/M3-rules.md step 4): rule CRUD with live
// preview and edit diff, conflict listing/resolution, all mutations in one
// transaction with re-evaluation and a single undoable action (RULE-5, D4).

// ruleBody is the request body of POST/PUT /api/rules and POST preview/diff.
type ruleBody struct {
	ID            *int64  `json:"id,omitempty"` // preview only: exclude this stored rule
	Name          *string `json:"name"`
	Pattern       *string `json:"pattern"`
	Direction     *string `json:"direction"`
	SubcategoryID *int64  `json:"subcategoryId"`
	AccountIDs    []int64 `json:"accountIds"`
	Enabled       *bool   `json:"enabled"`
}

// ruleJSON is the API representation of a rule (RULE-8).
type ruleJSON struct {
	ID               int64   `json:"id"`
	Name             string  `json:"name"`
	Pattern          string  `json:"pattern"`
	Direction        string  `json:"direction"`
	SubcategoryID    int64   `json:"subcategoryId"`
	SubcategoryName  *string `json:"subcategoryName"`
	SubcategoryColor *string `json:"subcategoryColor"`
	Enabled          bool    `json:"enabled"`
	AccountIDs       []int64 `json:"accountIds"`
	MatchCount       int64   `json:"matchCount"`
	CreatedAt        string  `json:"createdAt"`
	UpdatedAt        string  `json:"updatedAt"`
}

// ruleSnapshot is the whole-row rule snapshot recorded in the action log: the
// rule row plus its account filter (restored by the actions hook, plan D12).
type ruleSnapshot struct {
	dbq.Rule
	AccountIDs []int64 `json:"accountIds"`
}

// ruleRefJSON is a compact rule reference (conflict list, RULE-7).
type ruleRefJSON struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Pattern string `json:"pattern"`
}

// conflictJSON is one conflicted line with its matching rules (GET /api/conflicts).
type conflictJSON struct {
	Line             lineJSON      `json:"line"`
	Rules            []ruleRefJSON `json:"rules"`
	ResolutionRuleID *int64        `json:"resolutionRuleId"`
}

// ruleFields is the validated, normalized form of a ruleBody.
type ruleFields struct {
	name          string
	pattern       string
	direction     string
	subcategoryID int64
	accountIDs    []int64
	enabled       bool
}

// validateRuleBody validates a create/edit body and returns the normalized
// fields. The regex compiler error is returned for RULE-2.
func validateRuleBody(ctx context.Context, q *dbq.Queries, body *ruleBody, requireAll bool) (ruleFields, *APIError) {
	var out ruleFields
	fail := func(code, msg string) (ruleFields, *APIError) {
		return out, &APIError{Code: code, Message: msg}
	}
	if body.Pattern == nil || *body.Pattern == "" {
		return fail("bad_params", "pattern is required")
	}
	out.pattern = *body.Pattern
	if _, err := rules.Compile(out.pattern); err != nil {
		return fail("invalid_regex", "Invalid regular expression: "+err.Error())
	}
	out.direction = "both"
	if body.Direction != nil && *body.Direction != "" {
		out.direction = *body.Direction
	}
	if !rules.ValidDirection(out.direction) {
		return fail("bad_params", "direction must be one of in, out, both")
	}
	if requireAll {
		if body.Name == nil || strings.TrimSpace(*body.Name) == "" {
			return fail("bad_params", "name is required")
		}
		if body.SubcategoryID == nil {
			return fail("bad_params", "subcategoryId is required")
		}
	}
	if body.Name != nil {
		out.name = strings.TrimSpace(*body.Name)
	}
	if body.SubcategoryID != nil {
		out.subcategoryID = *body.SubcategoryID
		if _, err := q.GetSubcategory(ctx, out.subcategoryID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fail("not_found", "subcategory not found")
			}
			return fail("db_error", err.Error())
		}
	}
	out.enabled = true
	if body.Enabled != nil {
		out.enabled = *body.Enabled
	}
	out.accountIDs = body.AccountIDs
	if out.accountIDs == nil {
		out.accountIDs = []int64{}
	}
	for _, a := range out.accountIDs {
		if _, err := q.GetAccount(ctx, a); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fail("bad_params", fmt.Sprintf("account %d not found", a))
			}
			return fail("db_error", err.Error())
		}
	}
	return out, nil
}

// writeAPIError writes a validation APIError with the right status code.
func writeAPIError(w http.ResponseWriter, e *APIError) {
	status := http.StatusBadRequest
	if e.Code == "not_found" {
		status = http.StatusNotFound
	}
	writeError(w, status, *e)
}

// ruleToJSON loads account IDs and the live matchCount (D10) and builds the API shape.
func ruleToJSON(ctx context.Context, q *dbq.Queries, r dbq.ListRulesRow, conflictCounts map[int64]int) ruleJSON {
	accs, err := q.ListAccountsForRule(ctx, r.ID)
	if err != nil {
		accs = nil
	}
	accountIDs := append([]int64{}, accs...)
	categorized, err := q.CountLinesByRule(ctx, sql.NullInt64{Int64: r.ID, Valid: true})
	if err != nil {
		categorized = 0
	}
	return ruleJSON{
		ID: r.ID, Name: r.Name, Pattern: r.Pattern, Direction: r.Direction,
		SubcategoryID: r.SubcategoryID, SubcategoryName: nullStrPtr(r.SubcategoryName),
		SubcategoryColor: nullStrPtr(r.SubcategoryColor), Enabled: r.Enabled == 1,
		AccountIDs: accountIDs, MatchCount: categorized + int64(conflictCounts[r.ID]),
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func snapshotOf(ctx context.Context, q *dbq.Queries, ruleID int64) (ruleSnapshot, error) {
	r, err := q.GetRule(ctx, ruleID)
	if err != nil {
		return ruleSnapshot{}, err
	}
	accs, err := q.ListAccountsForRule(ctx, ruleID)
	if err != nil {
		return ruleSnapshot{}, err
	}
	snap := ruleSnapshot{Rule: *r, AccountIDs: append([]int64{}, accs...)}
	return snap, nil
}

// handleListRules: GET /api/rules (RULE-8). Filterable by enabled/subcategoryId/
// search, sortable server-side by name/pattern/subcategory/matchCount/createdAt.
func (s *Server) handleListRules() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		ctx := r.Context()
		q := dbq.New(d)
		params := dbq.ListRulesParams{}
		if v := r.URL.Query().Get("enabled"); v == "1" || v == "0" {
			params.Enabled = v
		}
		if v := r.URL.Query().Get("subcategoryId"); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				params.SubcategoryID = n
			}
		}
		params.Search = r.URL.Query().Get("search")

		rows, err := q.ListRules(ctx, params)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		conflictCounts, err := rules.ConflictCounts(ctx, q)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		out := make([]ruleJSON, 0, len(rows))
		for _, row := range rows {
			out = append(out, ruleToJSON(ctx, q, *row, conflictCounts))
		}

		sortKey := r.URL.Query().Get("sort")
		if sortKey == "" {
			sortKey = "name"
		}
		order := r.URL.Query().Get("order")
		if order == "" {
			order = "asc"
		}
		desc := order == "desc"
		sort.SliceStable(out, func(i, j int) bool {
			var less bool
			switch sortKey {
			case "pattern":
				less = out[i].Pattern < out[j].Pattern
			case "subcategory":
				less = nullStrVal(out[i].SubcategoryName) < nullStrVal(out[j].SubcategoryName)
			case "matchCount":
				less = out[i].MatchCount < out[j].MatchCount
			case "createdAt":
				less = out[i].CreatedAt < out[j].CreatedAt
			default:
				less = out[i].Name < out[j].Name
			}
			if desc {
				return !less && (out[i].ID != out[j].ID)
			}
			return less
		})
		writeJSON(w, http.StatusOK, orEmpty(out))
	}
}

// conflictLineJSON maps a ListConflictedLinesRow (same columns as GetStatementLineRow).
func conflictLineJSON(row *dbq.ListConflictedLinesRow) lineJSON {
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

func nullStrVal(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// handleCreateRule: POST /api/rules (RULE-1/2/5). One undoable action.
func (s *Server) handleCreateRule() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		ctx := r.Context()
		var body ruleBody
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_params", Message: "invalid JSON body"})
			return
		}
		f, errResp := validateRuleBody(ctx, dbq.New(d), &body, true)
		if errResp != nil {
			writeAPIError(w, errResp)
			return
		}

		tx, err := d.BeginTx(ctx, nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		defer tx.Rollback()
		q := dbq.New(tx)

		var en int64
		if f.enabled {
			en = 1
		}
		rule, err := q.CreateRule(ctx, dbq.CreateRuleParams{
			Name: f.name, Pattern: f.pattern, Direction: f.direction, SubcategoryID: f.subcategoryID, Enabled: en,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		for _, a := range f.accountIDs {
			if err := q.InsertRuleAccount(ctx, dbq.InsertRuleAccountParams{RuleID: rule.ID, AccountID: a}); err != nil {
				writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
				return
			}
		}
		// re-evaluate all candidate lines with the new rule (D4/D5)
		lineChanges, err := rules.EvaluateAll(ctx, q)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		snap, err := snapshotOf(ctx, q, rule.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		changes := append([]actions.Change{
			{Entity: actions.EntityRule, EntityID: rule.ID, Before: nil, After: snap},
		}, lineChanges...)
		desc := fmt.Sprintf("Created rule %q (%d lines affected)", f.name, len(lineChanges))
		if _, err := actions.Record(ctx, tx, "rule_create", desc, changes); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		if err := tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}

		conflictCounts, _ := rules.ConflictCounts(ctx, dbq.New(d))
		writeJSON(w, http.StatusCreated, ruleJSONFromRule(ctx, dbq.New(d), *rule, conflictCounts))
	}
}

// ruleJSONFromRule builds the API shape from a rule row (joining the subcategory).
func ruleJSONFromRule(ctx context.Context, q *dbq.Queries, r dbq.Rule, conflictCounts map[int64]int) ruleJSON {
	sub, err := q.GetSubcategory(ctx, r.SubcategoryID)
	var subName, subColor *string
	if err == nil {
		subName = &sub.Name
		subColor = &sub.Color
	}
	row := dbq.ListRulesRow{
		ID: r.ID, Name: r.Name, Pattern: r.Pattern, Direction: r.Direction,
		SubcategoryID: r.SubcategoryID, Enabled: r.Enabled, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		SubcategoryName:  sql.NullString{String: nullStrVal(subName), Valid: subName != nil},
		SubcategoryColor: sql.NullString{String: nullStrVal(subColor), Valid: subColor != nil},
	}
	return ruleToJSON(ctx, q, row, conflictCounts)
}

// handleGetRule: GET /api/rules/{id}.
func (s *Server) handleGetRule() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		ctx := r.Context()
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_id", Message: "invalid rule id"})
			return
		}
		q := dbq.New(d)
		rule, err := q.GetRule(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, APIError{Code: "not_found", Message: "rule not found"})
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		conflictCounts, _ := rules.ConflictCounts(ctx, q)
		writeJSON(w, http.StatusOK, ruleJSONFromRule(ctx, q, *rule, conflictCounts))
	}
}

// handleUpdateRule: PUT /api/rules/{id} (RULE-4/5/6). One undoable action.
func (s *Server) handleUpdateRule() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		ctx := r.Context()
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_id", Message: "invalid rule id"})
			return
		}
		var body ruleBody
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_params", Message: "invalid JSON body"})
			return
		}
		f, errResp := validateRuleBody(ctx, dbq.New(d), &body, true)
		if errResp != nil {
			writeAPIError(w, errResp)
			return
		}

		tx, err := d.BeginTx(ctx, nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		defer tx.Rollback()
		q := dbq.New(tx)
		if _, err := q.GetRule(ctx, id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, APIError{Code: "not_found", Message: "rule not found"})
				return
			}
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		beforeSnap, err := snapshotOf(ctx, q, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		var en int64
		if f.enabled {
			en = 1
		}
		if _, err := q.UpdateRule(ctx, dbq.UpdateRuleParams{
			Name: f.name, Pattern: f.pattern, Direction: f.direction, SubcategoryID: f.subcategoryID, Enabled: en, ID: id,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		if err := q.DeleteRuleAccountsForRule(ctx, id); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		for _, a := range f.accountIDs {
			if err := q.InsertRuleAccount(ctx, dbq.InsertRuleAccountParams{RuleID: id, AccountID: a}); err != nil {
				writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
				return
			}
		}
		lineChanges, err := rules.EvaluateAll(ctx, q)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		afterSnap, err := snapshotOf(ctx, q, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		changes := append([]actions.Change{
			{Entity: actions.EntityRule, EntityID: id, Before: beforeSnap, After: afterSnap},
		}, lineChanges...)
		desc := fmt.Sprintf("Edited rule %q (%d lines affected)", f.name, len(lineChanges))
		if _, err := actions.Record(ctx, tx, "rule_edit", desc, changes); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		if err := tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		conflictCounts, _ := rules.ConflictCounts(ctx, dbq.New(d))
		updated, err := dbq.New(d).GetRule(ctx, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, ruleJSONFromRule(ctx, dbq.New(d), *updated, conflictCounts))
	}
}

// handleToggleRule: POST /api/rules/{id}/toggle. Body {"enabled": bool} optional;
// without it the current state flips. One undoable action.
func (s *Server) handleToggleRule() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		ctx := r.Context()
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_id", Message: "invalid rule id"})
			return
		}
		var body struct {
			Enabled *bool `json:"enabled"`
		}
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_params", Message: "invalid JSON body"})
			return
		}

		tx, err := d.BeginTx(ctx, nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		defer tx.Rollback()
		q := dbq.New(tx)
		before, err := q.GetRule(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, APIError{Code: "not_found", Message: "rule not found"})
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		enabled := before.Enabled == 0
		if body.Enabled != nil {
			enabled = *body.Enabled
		}
		var en int64
		if enabled {
			en = 1
		}
		beforeSnap, err := snapshotOf(ctx, q, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		if err := q.SetRuleEnabled(ctx, dbq.SetRuleEnabledParams{Enabled: en, ID: id}); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		lineChanges, err := rules.EvaluateAll(ctx, q)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		afterSnap, err := snapshotOf(ctx, q, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		changes := append([]actions.Change{
			{Entity: actions.EntityRule, EntityID: id, Before: beforeSnap, After: afterSnap},
		}, lineChanges...)
		desc := fmt.Sprintf("%s rule %q (%d lines affected)", map[bool]string{true: "Enabled", false: "Disabled"}[enabled], before.Name, len(lineChanges))
		if _, err := actions.Record(ctx, tx, "rule_toggle", desc, changes); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		if err := tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		conflictCounts, _ := rules.ConflictCounts(ctx, dbq.New(d))
		updated, err := dbq.New(d).GetRule(ctx, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, ruleJSONFromRule(ctx, dbq.New(d), *updated, conflictCounts))
	}
}

// handleDeleteRule: DELETE /api/rules/{id} (RULE-6). One undoable action.
func (s *Server) handleDeleteRule() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		ctx := r.Context()
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_id", Message: "invalid rule id"})
			return
		}

		tx, err := d.BeginTx(ctx, nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		defer tx.Rollback()
		q := dbq.New(tx)
		beforeSnap, err := snapshotOf(ctx, q, id)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, APIError{Code: "not_found", Message: "rule not found"})
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		// delete first so the evaluator no longer sees the rule; cascades
		// rule_account and conflict_resolution (FK ON DELETE CASCADE)
		if err := q.DeleteRule(ctx, id); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		lineChanges, err := rules.EvaluateAll(ctx, q)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		changes := append([]actions.Change{
			{Entity: actions.EntityRule, EntityID: id, Before: beforeSnap, After: nil},
		}, lineChanges...)
		desc := fmt.Sprintf("Deleted rule %q (%d lines re-evaluated)", beforeSnap.Name, len(lineChanges))
		if _, err := actions.Record(ctx, tx, "rule_delete", desc, changes); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		if err := tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "id": id})
	}
}

// handlePreviewRules: POST /api/rules/preview (RULE-2/3). Read-only.
func (s *Server) handlePreviewRules() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		ctx := r.Context()
		var body ruleBody
		if err := decodeJSON(r, &body); err != nil || body.Pattern == nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_params", Message: "body must include a pattern"})
			return
		}
		f, errResp := validateRuleBody(ctx, dbq.New(d), &body, false)
		if errResp != nil {
			writeAPIError(w, errResp)
			return
		}
		var exclude int64
		if body.ID != nil {
			exclude = *body.ID
		}
		def := rules.Rule{
			ID: exclude, Name: f.name, Pattern: f.pattern, Direction: f.direction,
			SubcategoryID: f.subcategoryID, Enabled: f.enabled, AccountIDs: f.accountIDs,
		}
		res, err := rules.Preview(ctx, dbq.New(d), def, exclude)
		if err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "invalid_regex", Message: "Invalid regular expression: " + err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, res)
	}
}

// handleDiffRule: POST /api/rules/{id}/diff (RULE-4). Read-only.
func (s *Server) handleDiffRule() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		ctx := r.Context()
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_id", Message: "invalid rule id"})
			return
		}
		var body ruleBody
		if err := decodeJSON(r, &body); err != nil || body.Pattern == nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_params", Message: "body must include a pattern"})
			return
		}
		f, errResp := validateRuleBody(ctx, dbq.New(d), &body, false)
		if errResp != nil {
			writeAPIError(w, errResp)
			return
		}
		def := rules.Rule{
			ID: id, Name: f.name, Pattern: f.pattern, Direction: f.direction,
			SubcategoryID: f.subcategoryID, Enabled: f.enabled, AccountIDs: f.accountIDs,
		}
		if old, err := dbq.New(d).GetRule(ctx, id); err == nil {
			if f.name == "" {
				def.Name = old.Name
			}
			if f.subcategoryID == 0 {
				def.SubcategoryID = old.SubcategoryID
			}
		}
		res, err := rules.Diff(ctx, dbq.New(d), id, def)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, APIError{Code: "not_found", Message: "rule not found"})
			return
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "invalid_regex", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, res)
	}
}

// handleListConflicts: GET /api/conflicts (RULE-7).
func (s *Server) handleListConflicts() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		ctx := r.Context()
		q := dbq.New(d)
		rows, err := q.ListConflictedLines(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		resolutions, err := q.ListConflictResolutions(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		resByLine := map[int64]int64{}
		for _, cr := range resolutions {
			resByLine[cr.LineID] = cr.RuleID
		}
		out := make([]conflictJSON, 0, len(rows))
		for _, row := range rows {
			line := rules.Line{
				ID: row.ID, AccountID: row.AccountID, Message: row.Message,
				AmountMinor: row.AmountMinor, Source: row.Source,
				SubcategoryID: row.SubcategoryID, RuleID: row.RuleID,
			}
			matching, err := rules.MatchingEnabledRules(ctx, q, &line)
			if err != nil {
				writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
				return
			}
			ruleRefs := make([]ruleRefJSON, 0, len(matching))
			for _, m := range matching {
				ruleRefs = append(ruleRefs, ruleRefJSON{ID: m.ID, Name: m.Name, Pattern: m.Pattern})
			}
			cj := conflictJSON{
				Line:  conflictLineJSON(row),
				Rules: ruleRefs,
			}
			if rid, ok := resByLine[row.ID]; ok {
				cj.ResolutionRuleID = &rid
			}
			out = append(out, cj)
		}
		writeJSON(w, http.StatusOK, orEmpty(out))
	}
}

// resolveConflictBody is the body of POST /api/lines/{id}/resolve-conflict.
type resolveConflictBody struct {
	RuleID *int64 `json:"ruleId"`
}

// handleResolveConflict: POST /api/lines/{id}/resolve-conflict (RULE-7, D7).
// 409 when the line is not in conflict or the rule does not match it.
func (s *Server) handleResolveConflict() http.HandlerFunc {
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
		var body resolveConflictBody
		if err := decodeJSON(r, &body); err != nil || body.RuleID == nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_params", Message: "body must be {\"ruleId\": number}"})
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
		if before.Source != "conflict" {
			writeError(w, http.StatusConflict, APIError{Code: "not_in_conflict", Message: "line is not in conflict"})
			return
		}
		rule, err := rules.LoadRule(ctx, q, *body.RuleID)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, APIError{Code: "not_found", Message: "rule not found"})
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		re, err := rules.Compile(rule.Pattern)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		line := rules.Line{
			ID: before.ID, AccountID: before.AccountID, Message: before.Message,
			AmountMinor: before.AmountMinor, Source: before.Source,
			SubcategoryID: before.SubcategoryID, RuleID: before.RuleID,
		}
		if !rule.Enabled || !rules.Matches(rule, re, &line) {
			writeError(w, http.StatusConflict, APIError{Code: "rule_does_not_match", Message: "rule is disabled or does not match this line"})
			return
		}

		var beforeCR any
		if cr, err := q.GetConflictResolution(ctx, id); err == nil {
			beforeCR = *cr
		} else if !errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		cr, err := q.UpsertConflictResolution(ctx, dbq.UpsertConflictResolutionParams{LineID: id, RuleID: rule.ID})
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		if err := q.UpdateLineSource(ctx, dbq.UpdateLineSourceParams{
			SubcategoryID: sql.NullInt64{Int64: rule.SubcategoryID, Valid: true},
			Source:        "rule",
			RuleID:        sql.NullInt64{Int64: rule.ID, Valid: true},
			ID:            id,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		after, err := q.GetLineForAction(ctx, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		desc := fmt.Sprintf("Resolved conflict on line #%d with rule %q", id, rule.Name)
		if _, err := actions.Record(ctx, tx, "resolve_conflict", desc, []actions.Change{
			{Entity: actions.EntityConflictResolution, EntityID: id, Before: beforeCR, After: *cr},
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

// handleUnresolveConflict: DELETE /api/lines/{id}/resolve-conflict (RULE-7, D7).
// The line returns to conflict (or another matching rule) via re-evaluation.
func (s *Server) handleUnresolveConflict() http.HandlerFunc {
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
		cr, err := q.GetConflictResolution(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusConflict, APIError{Code: "no_resolution", Message: "line has no remembered conflict resolution"})
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		if err := q.DeleteConflictResolution(ctx, id); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		if _, err := rules.EvaluateLines(ctx, q, []int64{id}); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		after, err := q.GetLineForAction(ctx, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		desc := fmt.Sprintf("Removed conflict resolution on line #%d", id)
		if _, err := actions.Record(ctx, tx, "unresolve_conflict", desc, []actions.Change{
			{Entity: actions.EntityConflictResolution, EntityID: id, Before: *cr, After: nil},
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
