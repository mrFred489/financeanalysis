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
