package actions

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/mrFred489/financeanalysis/internal/db"
	"github.com/mrFred489/financeanalysis/internal/db/dbq"
)

// newTestDB opens a fresh migrated database in a temp dir.
func newTestDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	db.Close() // reset any global handle from a previous test
	dir := t.TempDir()
	path := filepath.Join(dir, "test.sqlite")
	if err := db.InitDB(context.Background(), path); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	d, err := db.GetDB()
	if err != nil {
		t.Fatalf("GetDB: %v", err)
	}
	if err := db.Migrate(context.Background(), d, filepath.Join(dir, "backups")); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return d, path
}

func insertAccount(t *testing.T, ctx context.Context, d *sql.DB, name string) int64 {
	t.Helper()
	res, err := d.ExecContext(ctx, "INSERT INTO account (name) VALUES (?)", name)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func insertLine(t *testing.T, ctx context.Context, d *sql.DB, accountID int64, seq int64, message string, amount int64) int64 {
	t.Helper()
	res, err := d.ExecContext(ctx, `
		INSERT INTO statement_line (account_id, booking_date, message, amount_minor, file_row, seq)
		VALUES (?, '2026-01-01', ?, ?, 1, ?)`, accountID, message, amount, seq)
	if err != nil {
		t.Fatalf("insert line: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func getLine(t *testing.T, ctx context.Context, q dbq.DBTX, id int64) dbq.StatementLine {
	t.Helper()
	row, err := dbq.New(q).GetLineForAction(ctx, id)
	if err != nil {
		t.Fatalf("GetLineForAction: %v", err)
	}
	return *row
}

// categorizeLine mutates the line like the handler does and records the action
// in one transaction, returning the action id.
func categorizeLine(t *testing.T, ctx context.Context, d *sql.DB, lineID, subID int64) int64 {
	t.Helper()
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()
	q := dbq.New(tx)
	before := getLine(t, ctx, tx, lineID)
	if _, err := q.SetLineSubcategory(ctx, dbq.SetLineSubcategoryParams{SubcategoryID: sql.NullInt64{Int64: subID, Valid: true}, ID: lineID}); err != nil {
		t.Fatalf("SetLineSubcategory: %v", err)
	}
	after := getLine(t, ctx, tx, lineID)
	id, err := Record(ctx, tx, "categorize", fmt.Sprintf("Categorized line #%d", lineID), []Change{
		{Entity: EntityStatementLine, EntityID: lineID, Before: before, After: after},
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return id
}

func subcategoryOf(t *testing.T, ctx context.Context, d *sql.DB, lineID int64) (sql.NullInt64, string) {
	t.Helper()
	row := getLine(t, ctx, d, lineID)
	return row.SubcategoryID, row.Source
}

func TestUndoRedoRoundTrip(t *testing.T) {
	ctx := context.Background()
	d, _ := newTestDB(t)
	acc := insertAccount(t, ctx, d, "Main")
	line := insertLine(t, ctx, d, acc, 1, "NETTO STORES", -100)

	categorizeLine(t, ctx, d, line, 6) // 6 = seeded "Groceries"

	sub, src := subcategoryOf(t, ctx, d, line)
	if !sub.Valid || sub.Int64 != 6 || src != "manual" {
		t.Fatalf("after categorize: sub=%v src=%q", sub, src)
	}

	action, err := Undo(ctx, d)
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if action.Kind != "categorize" || len(action.Changes) != 1 {
		t.Fatalf("unexpected action: %+v", action)
	}
	sub, src = subcategoryOf(t, ctx, d, line)
	if sub.Valid || src != "none" {
		t.Fatalf("after undo: sub=%v src=%q, want NULL/none", sub, src)
	}

	if _, err := Redo(ctx, d); err != nil {
		t.Fatalf("Redo: %v", err)
	}
	sub, src = subcategoryOf(t, ctx, d, line)
	if !sub.Valid || sub.Int64 != 6 || src != "manual" {
		t.Fatalf("after redo: sub=%v src=%q", sub, src)
	}

	// undo -> redo -> undo round trip
	if _, err := Undo(ctx, d); err != nil {
		t.Fatalf("Undo 2: %v", err)
	}
	sub, _ = subcategoryOf(t, ctx, d, line)
	if sub.Valid {
		t.Fatalf("after undo 2: sub=%v, want NULL", sub)
	}
}

func TestLIFOOrder(t *testing.T) {
	ctx := context.Background()
	d, _ := newTestDB(t)
	acc := insertAccount(t, ctx, d, "Main")
	l1 := insertLine(t, ctx, d, acc, 1, "A", -1)
	l2 := insertLine(t, ctx, d, acc, 2, "B", -2)
	l3 := insertLine(t, ctx, d, acc, 3, "C", -3)

	a1 := categorizeLine(t, ctx, d, l1, 6)
	a2 := categorizeLine(t, ctx, d, l2, 7)
	a3 := categorizeLine(t, ctx, d, l3, 8)

	got, err := Undo(ctx, d)
	if err != nil || got.ID != a3 {
		t.Fatalf("first undo: got=%v err=%v, want id %d", got, err, a3)
	}
	got, err = Undo(ctx, d)
	if err != nil || got.ID != a2 {
		t.Fatalf("second undo: got=%v err=%v, want id %d", got, err, a2)
	}
	got, err = Undo(ctx, d)
	if err != nil || got.ID != a1 {
		t.Fatalf("third undo: got=%v err=%v, want id %d", got, err, a1)
	}
	if _, err := Undo(ctx, d); !errors.Is(err, ErrNothingToUndo) {
		t.Fatalf("empty undo: err=%v, want ErrNothingToUndo", err)
	}
	// redo is also LIFO over the undone prefix
	got, err = Redo(ctx, d)
	if err != nil || got.ID != a1 {
		t.Fatalf("first redo: got=%v err=%v, want id %d", got, err, a1)
	}
}

func TestCapAtMaxActions(t *testing.T) {
	ctx := context.Background()
	d, _ := newTestDB(t)
	acc := insertAccount(t, ctx, d, "Main")
	line := insertLine(t, ctx, d, acc, 1, "A", -1)

	total := MaxActions + 5
	for i := 0; i < total; i++ {
		categorizeLine(t, ctx, d, line, int64(6+i%3))
	}
	list, err := List(ctx, d, 1000)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if int64(len(list)) != MaxActions {
		t.Fatalf("stack size = %d, want %d", len(list), MaxActions)
	}
	// latest first, and the oldest kept action is total-MaxActions+1
	if list[0].ID != int64(total) || list[len(list)-1].ID != int64(total-MaxActions+1) {
		t.Fatalf("bounds: first=%d last=%d, want %d..%d", list[0].ID, list[len(list)-1].ID, total, total-MaxActions+1)
	}
}

func TestBulkActionRestoresEveryRow(t *testing.T) {
	ctx := context.Background()
	d, _ := newTestDB(t)
	acc := insertAccount(t, ctx, d, "Main")
	ids := []int64{
		insertLine(t, ctx, d, acc, 1, "A", -1),
		insertLine(t, ctx, d, acc, 2, "B", -2),
		insertLine(t, ctx, d, acc, 3, "C", -3),
	}

	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	q := dbq.New(tx)
	var changes []Change
	for _, id := range ids {
		before := getLine(t, ctx, tx, id)
		if _, err := q.SetLineSubcategory(ctx, dbq.SetLineSubcategoryParams{SubcategoryID: sql.NullInt64{Int64: 6, Valid: true}, ID: id}); err != nil {
			t.Fatalf("SetLineSubcategory: %v", err)
		}
		after := getLine(t, ctx, tx, id)
		changes = append(changes, Change{Entity: EntityStatementLine, EntityID: id, Before: before, After: after})
	}
	actionID, err := Record(ctx, tx, "categorize_bulk", "Categorized 3 lines as Groceries", changes)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	action, err := Undo(ctx, d)
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if action.ID != actionID || len(action.Changes) != 3 {
		t.Fatalf("bulk action: id=%d changes=%d", action.ID, len(action.Changes))
	}
	for _, id := range ids {
		sub, src := subcategoryOf(t, ctx, d, id)
		if sub.Valid || src != "none" {
			t.Fatalf("line %d after bulk undo: sub=%v src=%q, want NULL/none", id, sub, src)
		}
	}
}

func TestPersistenceAcrossReopen(t *testing.T) {
	ctx := context.Background()
	d, path := newTestDB(t)
	acc := insertAccount(t, ctx, d, "Main")
	line := insertLine(t, ctx, d, acc, 1, "A", -1)
	categorizeLine(t, ctx, d, line, 6)

	// Close and reopen the same file; the undo stack must survive (UNDO-2).
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	d2, err := db.Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer d2.Close()

	sub, src := subcategoryOf(t, ctx, d2, line)
	if !sub.Valid || src != "manual" {
		t.Fatalf("after reopen: sub=%v src=%q", sub, src)
	}
	if _, err := Undo(ctx, d2); err != nil {
		t.Fatalf("Undo after reopen: %v", err)
	}
	sub, src = subcategoryOf(t, ctx, d2, line)
	if sub.Valid || src != "none" {
		t.Fatalf("after undo post-reopen: sub=%v src=%q, want NULL/none", sub, src)
	}
}

func TestEmptyStackErrors(t *testing.T) {
	ctx := context.Background()
	d, _ := newTestDB(t)

	if _, err := Undo(ctx, d); !errors.Is(err, ErrNothingToUndo) {
		t.Fatalf("Undo: err=%v, want ErrNothingToUndo", err)
	}
	if _, err := Redo(ctx, d); !errors.Is(err, ErrNothingToRedo) {
		t.Fatalf("Redo: err=%v, want ErrNothingToRedo", err)
	}

	// after one action + undo, redo works but a second redo has nothing left
	acc := insertAccount(t, ctx, d, "Main")
	line := insertLine(t, ctx, d, acc, 1, "A", -1)
	categorizeLine(t, ctx, d, line, 6)
	if _, err := Undo(ctx, d); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if _, err := Redo(ctx, d); err != nil {
		t.Fatalf("Redo: %v", err)
	}
	if _, err := Redo(ctx, d); !errors.Is(err, ErrNothingToRedo) {
		t.Fatalf("second Redo: err=%v, want ErrNothingToRedo", err)
	}
}

func TestListIncludesChangeCountAndUndoneFlag(t *testing.T) {
	ctx := context.Background()
	d, _ := newTestDB(t)
	acc := insertAccount(t, ctx, d, "Main")
	line := insertLine(t, ctx, d, acc, 1, "A", -1)
	categorizeLine(t, ctx, d, line, 6)

	list, err := List(ctx, d, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ChangeCount != 1 || list[0].UndoneAt.Valid {
		t.Fatalf("list: %+v", list)
	}
	if _, err := Undo(ctx, d); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	list, err = List(ctx, d, 10)
	if err != nil {
		t.Fatalf("List 2: %v", err)
	}
	if len(list) != 1 || !list[0].UndoneAt.Valid {
		t.Fatalf("list after undo: %+v", list)
	}
}

// --- M3 entities: rule and conflict_resolution (plan step 3, D12) ---

// ruleSnapshot is the whole-row rule snapshot shape recorded by the M3 server:
// the rule row plus its account filter as accountIds (restored by the hook).
type ruleSnapshot struct {
	dbq.Rule
	AccountIDs []int64 `json:"accountIds"`
}

func insertRule(t *testing.T, ctx context.Context, q dbq.DBTX, name, pattern string, subID int64, enabled bool, accountIDs ...int64) int64 {
	t.Helper()
	var en int64
	if enabled {
		en = 1
	}
	r, err := dbq.New(q).CreateRule(ctx, dbq.CreateRuleParams{
		Name: name, Pattern: pattern, Direction: "both", SubcategoryID: subID, Enabled: en,
	})
	if err != nil {
		t.Fatalf("CreateRule: %v", err)
	}
	for _, a := range accountIDs {
		if err := dbq.New(q).InsertRuleAccount(ctx, dbq.InsertRuleAccountParams{RuleID: r.ID, AccountID: a}); err != nil {
			t.Fatalf("InsertRuleAccount: %v", err)
		}
	}
	return r.ID
}

func ruleAccounts(t *testing.T, ctx context.Context, q dbq.DBTX, ruleID int64) []int64 {
	t.Helper()
	rows, err := dbq.New(q).ListAccountsForRule(ctx, ruleID)
	if err != nil {
		t.Fatalf("ListAccountsForRule: %v", err)
	}
	return append([]int64{}, rows...)
}

func ruleExists(t *testing.T, ctx context.Context, d *sql.DB, ruleID int64) bool {
	t.Helper()
	var n int64
	if err := d.QueryRowContext(ctx, "SELECT COUNT(*) FROM rule WHERE id = ?", ruleID).Scan(&n); err != nil {
		t.Fatalf("count rule: %v", err)
	}
	return n == 1
}

// TestUndoRuleCreate: undoing a rule create deletes the rule (and its account
// rows via cascade) and restores the affected lines; redo re-creates all.
func TestUndoRuleCreate(t *testing.T) {
	ctx := context.Background()
	d, _ := newTestDB(t)
	acc := insertAccount(t, ctx, d, "Main")
	line := insertLine(t, ctx, d, acc, 1, "NETTO STORES", -100)

	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()
	q := dbq.New(tx)
	ruleID := insertRule(t, ctx, tx, "Groceries", `^NETTO`, 6, true, acc)
	before := getLine(t, ctx, tx, line)
	if err := q.UpdateLineSource(ctx, dbq.UpdateLineSourceParams{
		SubcategoryID: sql.NullInt64{Int64: 6, Valid: true}, Source: "rule",
		RuleID: sql.NullInt64{Int64: ruleID, Valid: true}, ID: line,
	}); err != nil {
		t.Fatalf("UpdateLineSource: %v", err)
	}
	after := getLine(t, ctx, tx, line)
	ruleRow, err := q.GetRule(ctx, ruleID)
	if err != nil {
		t.Fatalf("GetRule: %v", err)
	}
	snapshot := ruleSnapshot{Rule: *ruleRow, AccountIDs: []int64{acc}}
	if _, err := Record(ctx, tx, "rule_create", "Created rule Groceries", []Change{
		{Entity: EntityRule, EntityID: ruleID, Before: nil, After: snapshot},
		{Entity: EntityStatementLine, EntityID: line, Before: before, After: after},
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if _, err := Undo(ctx, d); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if ruleExists(t, ctx, d, ruleID) {
		t.Errorf("undo of rule_create must delete the rule row")
	}
	if got := ruleAccounts(t, ctx, d, ruleID); len(got) != 0 {
		t.Errorf("rule_account rows must be gone after undo, got %v", got)
	}
	sub, src := subcategoryOf(t, ctx, d, line)
	if sub.Valid || src != "none" {
		t.Errorf("after undo: sub=%v src=%q, want NULL/none", sub, src)
	}

	if _, err := Redo(ctx, d); err != nil {
		t.Fatalf("Redo: %v", err)
	}
	if !ruleExists(t, ctx, d, ruleID) {
		t.Errorf("redo must re-create the rule row")
	}
	r, err := dbq.New(d).GetRule(ctx, ruleID)
	if err != nil || r.Pattern != "^NETTO" || r.Enabled != 1 {
		t.Errorf("redo rule row wrong: %+v err=%v", r, err)
	}
	if got := ruleAccounts(t, ctx, d, ruleID); len(got) != 1 || got[0] != acc {
		t.Errorf("redo must restore rule_account rows, got %v", got)
	}
	sub, src = subcategoryOf(t, ctx, d, line)
	if !sub.Valid || sub.Int64 != 6 || src != "rule" {
		t.Errorf("after redo: sub=%v src=%q, want 6/rule", sub, src)
	}
}

// TestUndoRuleEdit: undoing an edit restores the old pattern and the old
// account filter, plus the line assignments.
func TestUndoRuleEdit(t *testing.T) {
	ctx := context.Background()
	d, _ := newTestDB(t)
	acc := insertAccount(t, ctx, d, "Main")
	other := insertAccount(t, ctx, d, "Savings")
	line := insertLine(t, ctx, d, acc, 1, "BILKA STORE", -100)

	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	q := dbq.New(tx)
	ruleID := insertRule(t, ctx, tx, "Shops", `^NETTO`, 6, true, acc)
	if err := q.UpdateLineSource(ctx, dbq.UpdateLineSourceParams{
		SubcategoryID: sql.NullInt64{Int64: 6, Valid: true}, Source: "rule",
		RuleID: sql.NullInt64{Int64: ruleID, Valid: true}, ID: line,
	}); err != nil {
		t.Fatalf("UpdateLineSource: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// edit: new pattern + new account filter, line drops back to none
	tx, err = d.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()
	q = dbq.New(tx)
	beforeRule, _ := q.GetRule(ctx, ruleID)
	beforeLine := getLine(t, ctx, tx, line)
	if _, err := q.UpdateRule(ctx, dbq.UpdateRuleParams{
		Name: "Shops", Pattern: `^BILKA`, Direction: "both", SubcategoryID: 6, Enabled: 1, ID: ruleID,
	}); err != nil {
		t.Fatalf("UpdateRule: %v", err)
	}
	if err := q.DeleteRuleAccountsForRule(ctx, ruleID); err != nil {
		t.Fatalf("DeleteRuleAccountsForRule: %v", err)
	}
	if err := q.InsertRuleAccount(ctx, dbq.InsertRuleAccountParams{RuleID: ruleID, AccountID: other}); err != nil {
		t.Fatalf("InsertRuleAccount: %v", err)
	}
	if err := q.UpdateLineSource(ctx, dbq.UpdateLineSourceParams{
		SubcategoryID: sql.NullInt64{}, Source: "none", RuleID: sql.NullInt64{}, ID: line,
	}); err != nil {
		t.Fatalf("UpdateLineSource: %v", err)
	}
	afterRule, _ := q.GetRule(ctx, ruleID)
	afterLine := getLine(t, ctx, tx, line)
	if _, err := Record(ctx, tx, "rule_edit", "Edited rule Shops", []Change{
		{Entity: EntityRule, EntityID: ruleID, Before: ruleSnapshot{Rule: *beforeRule, AccountIDs: []int64{acc}}, After: ruleSnapshot{Rule: *afterRule, AccountIDs: []int64{other}}},
		{Entity: EntityStatementLine, EntityID: line, Before: beforeLine, After: afterLine},
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if _, err := Undo(ctx, d); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	r, err := dbq.New(d).GetRule(ctx, ruleID)
	if err != nil || r.Pattern != "^NETTO" {
		t.Errorf("undo must restore old pattern, got %+v err=%v", r, err)
	}
	if got := ruleAccounts(t, ctx, d, ruleID); len(got) != 1 || got[0] != acc {
		t.Errorf("undo must restore old account filter, got %v", got)
	}
	sub, src := subcategoryOf(t, ctx, d, line)
	if !sub.Valid || src != "rule" {
		t.Errorf("undo must restore line assignment, got sub=%v src=%q", sub, src)
	}
}

// TestUndoConflictResolution: resolving a conflict is undoable; undo restores
// the conflict state and removes the remembered resolution.
func TestUndoConflictResolution(t *testing.T) {
	ctx := context.Background()
	d, _ := newTestDB(t)
	acc := insertAccount(t, ctx, d, "Main")
	line := insertLine(t, ctx, d, acc, 1, "NETTO DINING", -100)
	// put the line into conflict
	if _, err := d.ExecContext(ctx, "UPDATE statement_line SET source = 'conflict' WHERE id = ?", line); err != nil {
		t.Fatalf("set conflict: %v", err)
	}

	ruleID := insertRule(t, ctx, d, "Dining", `DINING`, 7, true)

	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()
	q := dbq.New(tx)
	before := getLine(t, ctx, tx, line)
	cr, err := q.UpsertConflictResolution(ctx, dbq.UpsertConflictResolutionParams{LineID: line, RuleID: ruleID})
	if err != nil {
		t.Fatalf("UpsertConflictResolution: %v", err)
	}
	if err := q.UpdateLineSource(ctx, dbq.UpdateLineSourceParams{
		SubcategoryID: sql.NullInt64{Int64: 7, Valid: true}, Source: "rule",
		RuleID: sql.NullInt64{Int64: ruleID, Valid: true}, ID: line,
	}); err != nil {
		t.Fatalf("UpdateLineSource: %v", err)
	}
	after := getLine(t, ctx, tx, line)
	if _, err := Record(ctx, tx, "resolve_conflict", "Resolved conflict", []Change{
		{Entity: EntityConflictResolution, EntityID: line, Before: nil, After: *cr},
		{Entity: EntityStatementLine, EntityID: line, Before: before, After: after},
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if _, err := Undo(ctx, d); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if _, err := dbq.New(d).GetConflictResolution(ctx, line); err != sql.ErrNoRows {
		t.Errorf("undo must remove the conflict_resolution row, err=%v", err)
	}
	_, src := subcategoryOf(t, ctx, d, line)
	if src != "conflict" {
		t.Errorf("undo must restore source=conflict, got %q", src)
	}

	if _, err := Redo(ctx, d); err != nil {
		t.Fatalf("Redo: %v", err)
	}
	cr2, err := dbq.New(d).GetConflictResolution(ctx, line)
	if err != nil || cr2.RuleID != ruleID {
		t.Errorf("redo must restore the resolution, got %+v err=%v", cr2, err)
	}
	_, src = subcategoryOf(t, ctx, d, line)
	if src != "rule" {
		t.Errorf("redo must restore source=rule, got %q", src)
	}
}
