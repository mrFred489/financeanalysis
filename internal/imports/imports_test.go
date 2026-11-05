package imports

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/mrFred489/financeanalysis/internal/db"
	"github.com/mrFred489/financeanalysis/internal/db/dbq"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "csv", name))
	if err != nil {
		t.Fatalf("failed to read fixture %s: %v", name, err)
	}
	return data
}

// setup opens a fresh migrated database in a temp dir and creates one account.
func setup(t *testing.T) (context.Context, *dbq.Queries, []byte, string, int64) {
	t.Helper()
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "finance.sqlite")
	backupDir := filepath.Join(tmpDir, "backups")

	if err := db.InitDB(ctx, dbPath); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	d, err := db.GetDB()
	if err != nil {
		t.Fatalf("GetDB: %v", err)
	}
	t.Cleanup(func() { d.Close() })

	if err := db.Migrate(ctx, d, backupDir); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	q := dbq.New(d)
	acc, err := q.CreateAccount(ctx, dbq.CreateAccountParams{
		Name:          "Main account",
		AccountNumber: sql.NullString{String: "9999 0000000001", Valid: true},
		SortOrder:     0,
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	secret, err := NewSecret()
	if err != nil {
		t.Fatalf("NewSecret: %v", err)
	}
	return ctx, q, secret, backupDir, acc.ID
}

func TestPreviewNormal(t *testing.T) {
	ctx, q, secret, _, accountID := setup(t)

	res, err := Preview(ctx, q, secret, accountID, "normal.csv", fixture(t, "normal.csv"))
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if res.NewCount != 3 || res.DuplicateCount != 0 {
		t.Errorf("expected 3 new / 0 dup, got %d / %d", res.NewCount, res.DuplicateCount)
	}
	if res.DateFrom != "2024-12-29" || res.DateTo != "2024-12-31" {
		t.Errorf("expected date range 2024-12-29..2024-12-31, got %s..%s", res.DateFrom, res.DateTo)
	}
	// The v2 export has no balance column: exactly one info warning (IMP-7 skipped)
	if len(res.Warnings) != 1 || res.Warnings[0].Type != "no_balance" {
		t.Errorf("expected a single no_balance warning for a clean first import, got %v", res.Warnings)
	}
	// Account sanity: the file's account number matches the account (IMP-8)
	if len(res.FileAccounts) != 1 || res.FileAccounts[0] != "9999 0000000001" {
		t.Errorf("expected fileAccounts [9999 0000000001], got %v", res.FileAccounts)
	}
	for _, w := range res.Warnings {
		if w.Type == "account_mismatch" {
			t.Errorf("unexpected account_mismatch warning: %v", w)
		}
	}
	if res.PreviewToken == "" {
		t.Errorf("expected a preview token")
	}
}

func TestPreviewBalanceGaps(t *testing.T) {
	ctx, q, secret, _, accountID := setup(t)

	res, err := Preview(ctx, q, secret, accountID, "balance_gaps.csv", fixture(t, "balance_gaps.csv"))
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	var gaps int
	for _, w := range res.Warnings {
		if w.Type == "balance_gap" {
			gaps++
			if w.Row != 2 {
				t.Errorf("expected gap at row 2, got row %d", w.Row)
			}
		}
		if w.Type == "no_balance" {
			t.Errorf("file carries balances, no_balance warning should not appear: %v", w)
		}
	}
	if gaps != 1 {
		t.Errorf("expected 1 balance gap warning, got %d (%v)", gaps, res.Warnings)
	}
}

func TestPreviewAccountMismatch(t *testing.T) {
	ctx, q, secret, backupDir, accountID := setup(t)

	// Import normal.csv first (account 9999 0000000001)
	content := fixture(t, "normal.csv")
	prev, err := Preview(ctx, q, secret, accountID, "normal.csv", content)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	d, _ := db.GetDB()
	if _, err := Commit(ctx, d, secret, backupDir, accountID, "normal.csv", content, prev.PreviewToken); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// A v2 file whose account number does not match the selected account (IMP-8)
	mismatch := []byte(";Some payment;9999 0000000099;;-100,00;;;15-01-2025;15-01-2025;15-01-2025;0000000099;;;;;\n")
	res, err := Preview(ctx, q, secret, accountID, "mismatch.csv", mismatch)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	var mismatchWarns int
	for _, w := range res.Warnings {
		if w.Type == "account_mismatch" {
			mismatchWarns++
		}
	}
	if mismatchWarns == 0 {
		t.Errorf("expected account_mismatch warning, got %v", res.Warnings)
	}

	// Same file again -> duplicate_file warning + all lines duplicates (IMP-9, IMP-6)
	res2, err := Preview(ctx, q, secret, accountID, "normal.csv", content)
	if err != nil {
		t.Fatalf("Preview 2: %v", err)
	}
	if res2.DuplicateCount != 3 || res2.NewCount != 0 {
		t.Errorf("expected 0 new / 3 dup on re-import, got %d / %d", res2.NewCount, res2.DuplicateCount)
	}
	var dupFile bool
	for _, w := range res2.Warnings {
		if w.Type == "duplicate_file" {
			dupFile = true
		}
	}
	if !dupFile {
		t.Errorf("expected duplicate_file warning on identical SHA-256, got %v", res2.Warnings)
	}
}

func TestCommitAndUndoRoundTrip(t *testing.T) {
	ctx, q, secret, backupDir, accountID := setup(t)
	d, _ := db.GetDB()

	content := fixture(t, "normal.csv")
	prev, err := Preview(ctx, q, secret, accountID, "normal.csv", content)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}

	// Tampered token must be rejected
	if _, err := Commit(ctx, d, secret, backupDir, accountID, "normal.csv", content, "deadbeef"); err != ErrInvalidPreviewToken {
		t.Fatalf("expected ErrInvalidPreviewToken, got %v", err)
	}

	res, err := Commit(ctx, d, secret, backupDir, accountID, "normal.csv", content, prev.PreviewToken)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if res.LinesImported != 3 || res.DuplicatesSkipped != 0 || res.Uncategorized != 3 {
		t.Errorf("unexpected commit result: %+v", res)
	}

	// Lines are stored with source='none', chronological seq
	lines, err := q.ListStatementLines(ctx, dbq.ListStatementLinesParams{Limit: 100})
	if err != nil {
		t.Fatalf("ListStatementLines: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if lines[0].Source != "none" {
		t.Errorf("expected source 'none', got %q", lines[0].Source)
	}
	if lines[0].BookingDate != "2024-12-31" || lines[0].Seq != 3 {
		t.Errorf("expected newest first with seq 3, got %s seq %d", lines[0].BookingDate, lines[0].Seq)
	}
	// v2 export has no balance -> NULL; bank_ref from column 10 is stored
	if lines[0].BalanceMinor.Valid {
		t.Errorf("expected NULL balance, got %d", lines[0].BalanceMinor.Int64)
	}
	if !lines[0].BankRef.Valid || lines[0].BankRef.String != "0000000001" {
		t.Errorf("expected bank_ref 0000000001, got %+v", lines[0].BankRef)
	}

	// Backup was taken before commit (IMP-10)
	backups, err := filepath.Glob(filepath.Join(backupDir, "*pre-import*.sqlite"))
	if err != nil || len(backups) == 0 {
		t.Errorf("expected a pre-import backup, got %v (%v)", backups, err)
	}

	// Re-import: everything is a duplicate
	prev2, err := Preview(ctx, q, secret, accountID, "normal.csv", content)
	if err != nil {
		t.Fatalf("Preview 2: %v", err)
	}
	res2, err := Commit(ctx, d, secret, backupDir, accountID, "normal.csv", content, prev2.PreviewToken)
	if err != nil {
		t.Fatalf("Commit 2: %v", err)
	}
	if res2.LinesImported != 0 || res2.DuplicatesSkipped != 3 {
		t.Errorf("expected 0 imported / 3 skipped, got %d / %d", res2.LinesImported, res2.DuplicatesSkipped)
	}

	// Undo the latest import (IMP-13)
	undoneID, err := UndoLatest(ctx, d, backupDir)
	if err != nil {
		t.Fatalf("UndoLatest: %v", err)
	}
	if undoneID != res2.ImportID {
		t.Errorf("expected to undo import %d, got %d", res2.ImportID, undoneID)
	}
	n, err := q.CountLinesByImport(ctx, sql.NullInt64{Int64: res2.ImportID, Valid: true})
	if err != nil {
		t.Fatalf("CountLinesByImport: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 lines after undo, got %d", n)
	}

	// Undo again -> undoes the first import
	if _, err := UndoLatest(ctx, d, backupDir); err != nil {
		t.Fatalf("UndoLatest 2: %v", err)
	}
	lines, err = q.ListStatementLines(ctx, dbq.ListStatementLinesParams{Limit: 100})
	if err != nil {
		t.Fatalf("ListStatementLines: %v", err)
	}
	if len(lines) != 0 {
		t.Errorf("expected 0 lines after both undos, got %d", len(lines))
	}

	// Nothing left to undo
	if _, err := UndoLatest(ctx, d, backupDir); err != ErrNoImportToUndo {
		t.Errorf("expected ErrNoImportToUndo, got %v", err)
	}
}

// TestPreviewCommitRealSnippet runs the full preview+commit flow on the real
// headerless export snippet (PRD 7.2), including a transfer row (cols 2/3).
func TestPreviewCommitRealSnippet(t *testing.T) {
	ctx, q, secret, backupDir, accountID := setup(t)
	d, _ := db.GetDB()

	content := fixture(t, "format_v2_snippet.csv")
	prev, err := Preview(ctx, q, secret, accountID, "format_v2_snippet.csv", content)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if prev.NewCount != 12 || prev.DuplicateCount != 0 {
		t.Errorf("expected 12 new / 0 dup, got %d / %d (errors %v)", prev.NewCount, prev.DuplicateCount, prev.ParseErrors)
	}
	// oldest booking date (column 8) is 30-10-2026 (the 29-10 row is the transaction date)
	if prev.DateFrom != "2026-10-30" || prev.DateTo != "2026-11-05" {
		t.Errorf("expected 2026-10-30..2026-11-05, got %s..%s", prev.DateFrom, prev.DateTo)
	}
	// both account numbers of the transfer row are reported (sorted)
	if len(prev.FileAccounts) != 2 || prev.FileAccounts[0] != "9999 0000000001" || prev.FileAccounts[1] != "9999 0000000002" {
		t.Errorf("expected fileAccounts [9999 0000000001 9999 0000000002], got %v", prev.FileAccounts)
	}

	res, err := Commit(ctx, d, secret, backupDir, accountID, "format_v2_snippet.csv", content, prev.PreviewToken)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if res.LinesImported != 12 {
		t.Fatalf("expected 12 imported, got %d", res.LinesImported)
	}

	lines, err := q.ListStatementLines(ctx, dbq.ListStatementLinesParams{Limit: 100})
	if err != nil {
		t.Fatalf("ListStatementLines: %v", err)
	}
	if len(lines) != 12 {
		t.Fatalf("expected 12 lines, got %d", len(lines))
	}
	// newest first (2026-11-05), balance NULL, bank_ref stored
	if lines[0].BookingDate != "2026-11-05" || lines[0].BalanceMinor.Valid {
		t.Errorf("unexpected newest line: %+v", lines[0])
	}
	if !lines[0].BankRef.Valid || lines[0].BankRef.String != "1000000001" {
		t.Errorf("expected bank_ref 1000000001, got %+v", lines[0].BankRef)
	}
	// the transfer row keeps the counterparty account
	var tr *dbq.ListStatementLinesRow
	for _, l := range lines {
		if l.Message == "Person B" {
			tr = l
		}
	}
	if tr == nil {
		t.Fatalf("transfer row not found")
	}
	if !tr.CounterpartyAccount.Valid || tr.CounterpartyAccount.String != "9999 0000000002" {
		t.Errorf("expected counterparty 9999 0000000002, got %+v", tr.CounterpartyAccount)
	}
	if tr.AmountMinor != 450000 {
		t.Errorf("expected 450000, got %d", tr.AmountMinor)
	}
}

func TestPreviewEmptyFile(t *testing.T) {
	ctx, q, secret, _, accountID := setup(t)
	if _, err := Preview(ctx, q, secret, accountID, "empty.csv", fixture(t, "empty.csv")); err == nil {
		t.Fatalf("expected error for empty file")
	}
}
