// Package actions implements the persistent LIFO undo/redo engine
// (PRD UNDO-1..3; plan docs/plans/M2-manual-categorization.md step 2, D2/D8).
//
// Record writes the mutation's action_log row and its action_change snapshots in
// the caller's transaction (so a mutation and its undo record are atomic).
// Undo/Redo replay whole-row JSON snapshots by entity/id, so they work across
// reloads and restarts (UNDO-2). The stack is strictly LIFO and capped at
// MaxActions entries (UNDO-3). Imports keep their own undo and are not part of
// this stack (UNDO-4).
package actions

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/mrFred489/financeanalysis/internal/db/dbq"
)

// MaxActions is the cap of the undo stack (UNDO-3: at least the last 200 actions).
const MaxActions = 200

// ErrNothingToUndo is returned by Undo when the stack has no undoable action.
var ErrNothingToUndo = errors.New("nothing to undo")

// ErrNothingToRedo is returned by Redo when no action has been undone.
var ErrNothingToRedo = errors.New("nothing to redo")

// Entity names stored in action_change.entity.
const (
	EntityStatementLine      = "statement_line"
	EntityRule               = "rule"                // M3 (plan D12): snapshot includes accountIds
	EntityConflictResolution = "conflict_resolution" // M3 (plan D7): keyed by line_id
)

// entityColumns whitelists the restorable columns per entity. Snapshots restore
// only known columns; migrations must not drop/rename these columns
// (docs/migrations.md section 8). The entity name itself only ever comes from
// this map's keys, so building the statements from it is safe.
var entityColumns = map[string][]string{
	EntityStatementLine: {
		"id", "account_id", "import_id", "booking_date", "transaction_date", "value_date",
		"message", "amount_minor", "balance_minor", "currency", "bank_ref", "counterparty_account",
		"file_row", "seq", "occurrence", "subcategory_id", "source", "rule_id", "spread_months",
	},
	EntityRule: {
		"id", "name", "pattern", "direction", "subcategory_id", "enabled", "created_at", "updated_at",
	},
	EntityConflictResolution: {
		"line_id", "rule_id", "created_at",
	},
}

// entityKey is the column that identifies a row of the entity (its PK).
var entityKey = map[string]string{
	EntityStatementLine:      "id",
	EntityRule:               "id",
	EntityConflictResolution: "line_id",
}

// Change is one affected row of an action. Before/After are full row structs
// (e.g. dbq.StatementLine); they are serialized to JSON for storage.
type Change struct {
	Entity   string
	EntityID int64
	Before   any
	After    any
}

// ActionChange is one stored row snapshot of an action.
type ActionChange struct {
	ActionID   int64  `json:"actionId"`
	Entity     string `json:"entity"`
	EntityID   int64  `json:"entityId"`
	BeforeJson string `json:"beforeJson"`
	AfterJson  string `json:"afterJson"`
}

// Action is one undoable user action with its row snapshots.
type Action struct {
	ID          int64          `json:"id"`
	Kind        string         `json:"kind"`
	Description string         `json:"description"`
	CreatedAt   string         `json:"createdAt"`
	UndoneAt    sql.NullString `json:"undoneAt"`
	ChangeCount int64          `json:"changeCount"`
	Changes     []ActionChange `json:"changes,omitempty"`
}

// Record inserts the action_log row and its action_change rows on the given
// DBTX (usually the handler's transaction, so mutation + record are atomic),
// then prunes the stack to MaxActions. Returns the new action id.
func Record(ctx context.Context, q dbq.DBTX, kind, description string, changes []Change) (int64, error) {
	queries := dbq.New(q)
	action, err := queries.InsertAction(ctx, dbq.InsertActionParams{Kind: kind, Description: description})
	if err != nil {
		return 0, fmt.Errorf("insert action: %w", err)
	}
	for _, c := range changes {
		if _, ok := entityColumns[c.Entity]; !ok {
			return 0, fmt.Errorf("unknown action entity %q", c.Entity)
		}
		before, err := json.Marshal(c.Before)
		if err != nil {
			return 0, fmt.Errorf("marshal before snapshot: %w", err)
		}
		after, err := json.Marshal(c.After)
		if err != nil {
			return 0, fmt.Errorf("marshal after snapshot: %w", err)
		}
		if err := queries.InsertActionChange(ctx, dbq.InsertActionChangeParams{
			ActionID:   action.ID,
			Entity:     c.Entity,
			EntityID:   c.EntityID,
			BeforeJson: string(before),
			AfterJson:  string(after),
		}); err != nil {
			return 0, fmt.Errorf("insert action change: %w", err)
		}
	}
	if err := Prune(ctx, q); err != nil {
		return 0, err
	}
	return action.ID, nil
}

// Prune deletes all but the newest MaxActions actions (cascade removes changes).
func Prune(ctx context.Context, q dbq.DBTX) error {
	if err := dbq.New(q).DeleteOldestActions(ctx, MaxActions); err != nil {
		return fmt.Errorf("prune actions: %w", err)
	}
	return nil
}

// List returns the most recent actions, latest first (UNDO-5).
func List(ctx context.Context, db *sql.DB, limit int) ([]Action, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := dbq.New(db).ListActions(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("list actions: %w", err)
	}
	out := make([]Action, 0, len(rows))
	for _, r := range rows {
		out = append(out, Action{
			ID: r.ID, Kind: r.Kind, Description: r.Description,
			CreatedAt: r.CreatedAt, UndoneAt: r.UndoneAt, ChangeCount: r.ChangeCount,
		})
	}
	return out, nil
}

// Undo reverts the latest not-yet-undone action (strictly LIFO) and marks it
// undone. Runs in its own transaction. Returns ErrNothingToUndo when empty.
func Undo(ctx context.Context, db *sql.DB) (*Action, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	action, err := undo(ctx, tx)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return action, nil
}

// Redo re-applies the latest undone action and clears its undone flag.
// Returns ErrNothingToRedo when there is nothing to redo.
func Redo(ctx context.Context, db *sql.DB) (*Action, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	action, err := redo(ctx, tx)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return action, nil
}

func undo(ctx context.Context, q dbq.DBTX) (*Action, error) {
	queries := dbq.New(q)
	latest, err := queries.GetLatestUndoableAction(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNothingToUndo
	}
	if err != nil {
		return nil, fmt.Errorf("get latest undoable action: %w", err)
	}
	changes, err := queries.ListActionChanges(ctx, latest.ID)
	if err != nil {
		return nil, fmt.Errorf("list action changes: %w", err)
	}
	for _, c := range changes {
		if err := applySnapshot(ctx, q, c.Entity, c.EntityID, c.BeforeJson); err != nil {
			return nil, err
		}
	}
	if err := queries.MarkActionUndone(ctx, latest.ID); err != nil {
		return nil, fmt.Errorf("mark action undone: %w", err)
	}
	return actionWithChanges(latest, changes), nil
}

func redo(ctx context.Context, q dbq.DBTX) (*Action, error) {
	queries := dbq.New(q)
	latest, err := queries.GetLatestUndoneAction(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNothingToRedo
	}
	if err != nil {
		return nil, fmt.Errorf("get latest undone action: %w", err)
	}
	changes, err := queries.ListActionChanges(ctx, latest.ID)
	if err != nil {
		return nil, fmt.Errorf("list action changes: %w", err)
	}
	for _, c := range changes {
		if err := applySnapshot(ctx, q, c.Entity, c.EntityID, c.AfterJson); err != nil {
			return nil, err
		}
	}
	if err := queries.MarkActionRedone(ctx, latest.ID); err != nil {
		return nil, fmt.Errorf("mark action redone: %w", err)
	}
	return actionWithChanges(latest, changes), nil
}

func actionWithChanges(row *dbq.ActionLog, changes []*dbq.ActionChange) *Action {
	a := &Action{
		ID: row.ID, Kind: row.Kind, Description: row.Description,
		CreatedAt: row.CreatedAt, UndoneAt: row.UndoneAt, ChangeCount: int64(len(changes)),
	}
	for _, c := range changes {
		a.Changes = append(a.Changes, ActionChange{
			ActionID: c.ActionID, Entity: c.Entity, EntityID: c.EntityID,
			BeforeJson: c.BeforeJson, AfterJson: c.AfterJson,
		})
	}
	return a
}

// applySnapshot restores one row from a whole-row JSON snapshot. A literal
// "null" snapshot means the row did not exist in that state (e.g. undoing a
// create), so the row is deleted. Otherwise the row is upserted by its entity
// key with the whitelisted columns present in the snapshot, so undo can also
// restore a previously deleted row (e.g. redoing a delete).
//
// Custom hook (plan D12): a "rule" snapshot carries the account filter as an
// "accountIds" JSON array; after upserting the rule row, rule_account is
// re-synced from it (delete + insert), because rule_account has no single id
// column of its own.
func applySnapshot(ctx context.Context, q dbq.DBTX, entity string, entityID int64, snapshot string) error {
	columns, ok := entityColumns[entity]
	if !ok {
		return fmt.Errorf("unknown action entity %q", entity)
	}
	key := entityKey[entity]
	dec := json.NewDecoder(strings.NewReader(snapshot))
	dec.UseNumber()
	var row map[string]any
	if err := dec.Decode(&row); err != nil {
		return fmt.Errorf("decode %s snapshot for id %d: %w", entity, entityID, err)
	}
	if row == nil {
		// "null" snapshot: the row must not exist in this state
		stmt := fmt.Sprintf(`DELETE FROM "%s" WHERE "%s" = ?`, entity, key)
		if _, err := q.ExecContext(ctx, stmt, entityID); err != nil {
			return fmt.Errorf("delete %s id %d: %w", entity, entityID, err)
		}
		return nil
	}
	cols := make([]string, 0, len(columns))
	args := make([]any, 0, len(columns))
	for _, col := range columns {
		v, present := row[col]
		if !present {
			continue // snapshot from an older schema: restore only known columns
		}
		cols = append(cols, `"`+col+`"`)
		args = append(args, jsonValue(v))
	}
	if len(cols) == 0 {
		return fmt.Errorf("snapshot for %s id %d has no restorable columns", entity, entityID)
	}
	setCols := make([]string, 0, len(cols))
	for _, c := range cols {
		if c == `"`+key+`"` {
			continue
		}
		setCols = append(setCols, c+" = excluded."+c)
	}
	var stmt string
	if len(setCols) == 0 {
		// only the key column: nothing to update on conflict
		stmt = fmt.Sprintf(`INSERT OR IGNORE INTO "%s" (%s) VALUES (%s)`,
			entity, strings.Join(cols, ", "), placeholders(len(cols)))
	} else {
		stmt = fmt.Sprintf(`INSERT INTO "%s" (%s) VALUES (%s) ON CONFLICT("%s") DO UPDATE SET %s`,
			entity, strings.Join(cols, ", "), placeholders(len(cols)), key, strings.Join(setCols, ", "))
	}
	if _, err := q.ExecContext(ctx, stmt, args...); err != nil {
		return fmt.Errorf("restore %s id %d: %w", entity, entityID, err)
	}
	if entity == EntityRule {
		if err := resyncRuleAccounts(ctx, q, entityID, row["accountIds"]); err != nil {
			return err
		}
	}
	return nil
}

func placeholders(n int) string {
	s := make([]string, n)
	for i := range s {
		s[i] = "?"
	}
	return strings.Join(s, ", ")
}

// resyncRuleAccounts replaces the rule_account rows of a rule from the
// "accountIds" array of a rule snapshot (plan D12). A missing key leaves the
// current rows untouched (snapshots from before M3).
func resyncRuleAccounts(ctx context.Context, q dbq.DBTX, ruleID int64, accountIDs any) error {
	arr, ok := accountIDs.([]any)
	if !ok {
		return nil
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM rule_account WHERE rule_id = ?`, ruleID); err != nil {
		return fmt.Errorf("clear rule_account for rule %d: %w", ruleID, err)
	}
	for _, v := range arr {
		id := jsonValue(v)
		if _, err := q.ExecContext(ctx, `INSERT INTO rule_account (rule_id, account_id) VALUES (?, ?)`, ruleID, id); err != nil {
			return fmt.Errorf("restore rule_account for rule %d: %w", ruleID, err)
		}
	}
	return nil
}

// jsonValue converts a decoded JSON value into a driver-friendly Go value,
// keeping whole numbers as int64 so STRICT INTEGER columns accept them.
// sql.Null* structs marshal as {"String"|"Int64": ..., "Valid": bool}, so
// those objects are unwrapped to their plain value (nil when not Valid).
func jsonValue(v any) any {
	if m, ok := v.(map[string]any); ok {
		if valid, ok := m["Valid"].(bool); ok && !valid {
			return nil
		}
		if s, ok := m["String"]; ok {
			return jsonValue(s)
		}
		if n, ok := m["Int64"]; ok {
			return jsonValue(n)
		}
		return nil
	}
	if n, ok := v.(json.Number); ok {
		if i, err := n.Int64(); err == nil {
			return i
		}
		if f, err := n.Float64(); err == nil {
			return f
		}
		return n.String()
	}
	return v
}
