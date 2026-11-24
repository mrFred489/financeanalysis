package transfers

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/mrFred489/financeanalysis/internal/db"
	"github.com/mrFred489/financeanalysis/internal/db/dbq"
)

// setup opens a fresh migrated database in a temp dir with two accounts.
func setup(t *testing.T) (context.Context, *sql.DB, *dbq.Queries, int64, int64) {
	t.Helper()
	ctx := context.Background()
	tmpDir := t.TempDir()
	if err := db.InitDB(ctx, filepath.Join(tmpDir, "finance.sqlite")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	d, err := db.GetDB()
	if err != nil {
		t.Fatalf("GetDB: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.Migrate(ctx, d, filepath.Join(tmpDir, "backups")); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	q := dbq.New(d)
	acc1, err := q.CreateAccount(ctx, dbq.CreateAccountParams{Name: "Main", AccountNumber: sql.NullString{String: "9999 0000000001", Valid: true}})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	acc2, err := q.CreateAccount(ctx, dbq.CreateAccountParams{Name: "Savings", AccountNumber: sql.NullString{String: "9999 0000000002", Valid: true}})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	return ctx, d, q, acc1.ID, acc2.ID
}

// addLine inserts one statement line on the given account.
func addLine(t *testing.T, ctx context.Context, q *dbq.Queries, accountID int64, date, message string, amount int64) int64 {
	t.Helper()
	id, err := q.InsertStatementLine(ctx, dbq.InsertStatementLineParams{
		AccountID: accountID, BookingDate: date, Message: message,
		AmountMinor: amount, Currency: "DKK", FileRow: 1, Seq: 1, Occurrence: 1,
	})
	if err != nil {
		t.Fatalf("InsertStatementLine: %v", err)
	}
	return id
}

func linkExists(t *testing.T, ctx context.Context, q *dbq.Queries, outID, inID int64) bool {
	t.Helper()
	n, err := q.TransferRejectionExists(ctx, dbq.TransferRejectionExistsParams{OutLineID: outID, InLineID: inID})
	if err != nil {
		t.Fatalf("TransferRejectionExists: %v", err)
	}
	return n > 0
}

func getLinkByLine(t *testing.T, ctx context.Context, q *dbq.Queries, lineID int64) *dbq.TransferLink {
	t.Helper()
	row, err := q.GetTransferLinkByLine(ctx, dbq.GetTransferLinkByLineParams{OutLineID: lineID, InLineID: lineID})
	if err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		t.Fatalf("GetTransferLinkByLine: %v", err)
	}
	return row
}

func lineSource(t *testing.T, ctx context.Context, q *dbq.Queries, id int64) string {
	t.Helper()
	row, err := q.GetLineForAction(ctx, id)
	if err != nil {
		t.Fatalf("GetLineForAction: %v", err)
	}
	return row.Source
}

func TestDetectUniqueOneToOneAutoLinked(t *testing.T) {
	ctx, _, q, acc1, acc2 := setup(t)
	out := addLine(t, ctx, q, acc1, "2026-01-05", "Overførsel", -50000)
	in := addLine(t, ctx, q, acc2, "2026-01-05", "Overførsel", 50000)

	auto, ambiguous, err := Detect(ctx, q)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(auto) != 1 || auto[0].OutLineID != out || auto[0].InLineID != in {
		t.Fatalf("expected auto pair %d->%d, got %+v", out, in, auto)
	}
	if len(ambiguous) != 0 {
		t.Fatalf("expected no ambiguous groups, got %+v", ambiguous)
	}

	changes, err := ApplyAutoLinks(ctx, q, auto)
	if err != nil {
		t.Fatalf("ApplyAutoLinks: %v", err)
	}
	if len(changes) != 3 { // two lines + one link row
		t.Fatalf("expected 3 changes, got %d", len(changes))
	}
	if src := lineSource(t, ctx, q, out); src != "transfer" {
		t.Errorf("out line source = %q, want transfer", src)
	}
	if src := lineSource(t, ctx, q, in); src != "transfer" {
		t.Errorf("in line source = %q, want transfer", src)
	}
	link := getLinkByLine(t, ctx, q, out)
	if link == nil || link.Method != "auto" || link.OutLineID != out || link.InLineID != in {
		t.Fatalf("expected auto link %d->%d, got %+v", out, in, link)
	}
}

func TestDetectAmbiguousNotLinked(t *testing.T) {
	ctx, _, q, acc1, acc2 := setup(t)
	out := addLine(t, ctx, q, acc1, "2026-01-05", "Overførsel", -50000)
	in1 := addLine(t, ctx, q, acc2, "2026-01-05", "Overførsel A", 50000)
	in2 := addLine(t, ctx, q, acc2, "2026-01-05", "Overførsel B", 50000)

	auto, ambiguous, err := Detect(ctx, q)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(auto) != 0 {
		t.Fatalf("ambiguous pair must not be auto-linked, got %+v", auto)
	}
	if len(ambiguous) != 1 || ambiguous[0].OutLineID != out {
		t.Fatalf("expected one ambiguous group for out %d, got %+v", out, ambiguous)
	}
	if len(ambiguous[0].CandidateInLineIDs) != 2 {
		t.Fatalf("expected 2 candidates, got %v", ambiguous[0].CandidateInLineIDs)
	}
	_ = in1
	_ = in2
}

func TestDetectOneSidedAmbiguousNotLinked(t *testing.T) {
	// out-line has exactly one candidate, but that in-line has two candidate
	// out-lines: not a unique 1:1, so no auto-link (TRF-2).
	ctx, _, q, acc1, acc2 := setup(t)
	out1 := addLine(t, ctx, q, acc1, "2026-01-05", "A", -50000)
	out2 := addLine(t, ctx, q, acc1, "2026-01-05", "B", -50000)
	in := addLine(t, ctx, q, acc2, "2026-01-05", "In", 50000)

	auto, ambiguous, err := Detect(ctx, q)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(auto) != 0 {
		t.Fatalf("expected no auto-link (not unique 1:1), got %+v", auto)
	}
	if len(ambiguous) != 2 {
		t.Fatalf("expected 2 ambiguous groups (one per out-line), got %+v", ambiguous)
	}
	_ = out1
	_ = out2
	_ = in
}

func TestDetectExclusions(t *testing.T) {
	ctx, _, q, acc1, acc2 := setup(t)

	// same account: excluded (TRF-1)
	sameOut := addLine(t, ctx, q, acc1, "2026-01-06", "same acct out", -10000)
	sameIn := addLine(t, ctx, q, acc1, "2026-01-06", "same acct in", 10000)
	// different date: excluded
	dateOut := addLine(t, ctx, q, acc1, "2026-01-07", "date out", -20000)
	dateIn := addLine(t, ctx, q, acc2, "2026-01-08", "date in", 20000)
	// same sign (not opposite): excluded
	posOut := addLine(t, ctx, q, acc1, "2026-01-09", "pos", 30000)
	posIn := addLine(t, ctx, q, acc2, "2026-01-09", "pos2", 30000)
	// zero amount: excluded
	zero := addLine(t, ctx, q, acc1, "2026-01-10", "zero", 0)
	zeroIn := addLine(t, ctx, q, acc2, "2026-01-10", "zero in", 0)
	// rejected pair: excluded
	rejOut := addLine(t, ctx, q, acc1, "2026-01-11", "rej out", -40000)
	rejIn := addLine(t, ctx, q, acc2, "2026-01-11", "rej in", 40000)
	if err := q.InsertTransferRejection(ctx, dbq.InsertTransferRejectionParams{OutLineID: rejOut, InLineID: rejIn}); err != nil {
		t.Fatalf("InsertTransferRejection: %v", err)
	}
	// already linked pair: excluded from candidates
	linkedOut := addLine(t, ctx, q, acc1, "2026-01-12", "linked out", -60000)
	linkedIn := addLine(t, ctx, q, acc2, "2026-01-12", "linked in", 60000)
	if _, err := ApplyAutoLinks(ctx, q, []Pair{{OutLineID: linkedOut, InLineID: linkedIn}}); err != nil {
		t.Fatalf("ApplyAutoLinks: %v", err)
	}

	auto, ambiguous, err := Detect(ctx, q)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(auto) != 0 || len(ambiguous) != 0 {
		t.Fatalf("expected no auto pairs and no ambiguous groups, got auto=%+v ambiguous=%+v", auto, ambiguous)
	}
	for _, id := range []int64{sameOut, sameIn, dateOut, dateIn, posOut, posIn, zero, zeroIn, rejOut, rejIn, linkedOut, linkedIn} {
		if link := getLinkByLine(t, ctx, q, id); link != nil && id != linkedOut && id != linkedIn {
			t.Errorf("line %d unexpectedly linked: %+v", id, link)
		}
	}
	if !linkExists(t, ctx, q, rejOut, rejIn) {
		t.Errorf("rejection row missing")
	}
}

func TestDetectIdempotent(t *testing.T) {
	ctx, d, q, acc1, acc2 := setup(t)
	out := addLine(t, ctx, q, acc1, "2026-01-05", "Overførsel", -50000)
	in := addLine(t, ctx, q, acc2, "2026-01-05", "Overførsel", 50000)

	auto1, _, err := Detect(ctx, q)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if _, err := ApplyAutoLinks(ctx, q, auto1); err != nil {
		t.Fatalf("ApplyAutoLinks: %v", err)
	}
	// Running detection again must not re-link or report the pair again.
	auto2, ambiguous2, err := Detect(ctx, q)
	if err != nil {
		t.Fatalf("Detect 2: %v", err)
	}
	if len(auto2) != 0 || len(ambiguous2) != 0 {
		t.Fatalf("expected idempotent detection, got auto=%+v ambiguous=%+v", auto2, ambiguous2)
	}
	// DetectAll convenience wrapper: nothing left to link.
	a, amb, err := DetectAll(ctx, d)
	if err != nil {
		t.Fatalf("DetectAll: %v", err)
	}
	if a != 0 || amb != 0 {
		t.Fatalf("DetectAll should be a no-op, got auto=%d ambiguous=%d", a, amb)
	}
	links, err := q.ListTransferLinks(ctx)
	if err != nil {
		t.Fatalf("ListTransferLinks: %v", err)
	}
	if len(links) != 1 || links[0].OutLineID != out || links[0].InLineID != in {
		t.Fatalf("expected exactly one link, got %+v", links)
	}
}

func TestDetectAllAppliesAutoLinks(t *testing.T) {
	ctx, d, q, acc1, acc2 := setup(t)
	addLine(t, ctx, q, acc1, "2026-02-01", "Out", -12345)
	addLine(t, ctx, q, acc2, "2026-02-01", "In", 12345)
	addLine(t, ctx, q, acc1, "2026-02-02", "Amb out", -999)
	addLine(t, ctx, q, acc2, "2026-02-02", "Amb in 1", 999)
	addLine(t, ctx, q, acc2, "2026-02-02", "Amb in 2", 999)

	autoCount, ambiguousCount, err := DetectAll(ctx, d)
	if err != nil {
		t.Fatalf("DetectAll: %v", err)
	}
	if autoCount != 1 || ambiguousCount != 1 {
		t.Fatalf("expected auto=1 ambiguous=1, got auto=%d ambiguous=%d", autoCount, ambiguousCount)
	}
}

func TestCandidatesForOutLine(t *testing.T) {
	ctx, _, q, acc1, acc2 := setup(t)
	out := addLine(t, ctx, q, acc1, "2026-03-01", "Out", -700)
	addLine(t, ctx, q, acc2, "2026-03-01", "In 1", 700)
	addLine(t, ctx, q, acc2, "2026-03-01", "In 2", 700)
	addLine(t, ctx, q, acc2, "2026-03-02", "Other date", 700)

	cands, err := CandidatesForOutLine(ctx, q, out)
	if err != nil {
		t.Fatalf("CandidatesForOutLine: %v", err)
	}
	if len(cands) != 2 {
		t.Fatalf("expected 2 candidates for out line, got %v", cands)
	}
}
