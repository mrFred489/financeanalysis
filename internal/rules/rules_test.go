package rules

import (
	"context"
	"database/sql"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mrFred489/financeanalysis/internal/db"
	"github.com/mrFred489/financeanalysis/internal/db/dbq"
)

// setup opens a fresh migrated database in a temp dir with two accounts and
// returns the queries plus the seeded subcategory ids.
func setup(t *testing.T) (context.Context, *dbq.Queries, int64, int64, int64, int64) {
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
	subs, err := q.ListSubcategories(ctx)
	if err != nil {
		t.Fatalf("ListSubcategories: %v", err)
	}
	byName := map[string]int64{}
	for _, s := range subs {
		byName[s.Name] = s.ID
	}
	groceries, ok := byName["Groceries"]
	if !ok {
		t.Fatalf("seeded subcategory Groceries not found")
	}
	dining := byName["Dining"]
	return ctx, q, acc1.ID, acc2.ID, groceries, dining
}

// addLine inserts one statement line with the given source and returns its id.
func addLine(t *testing.T, ctx context.Context, q *dbq.Queries, accountID int64, message string, amount int64, source string) int64 {
	t.Helper()
	id, err := q.InsertStatementLine(ctx, dbq.InsertStatementLineParams{
		AccountID: accountID, BookingDate: "2026-01-05", Message: message,
		AmountMinor: amount, Currency: "DKK", FileRow: 1, Seq: 1, Occurrence: 1,
	})
	if err != nil {
		t.Fatalf("InsertStatementLine: %v", err)
	}
	if source != "none" {
		if err := q.UpdateLineSource(ctx, dbq.UpdateLineSourceParams{
			SubcategoryID: sql.NullInt64{}, Source: source, RuleID: sql.NullInt64{}, ID: id,
		}); err != nil {
			t.Fatalf("UpdateLineSource: %v", err)
		}
	}
	return id
}

// addRule creates a rule row plus account filters.
func addRule(t *testing.T, ctx context.Context, q *dbq.Queries, name, pattern, direction string, subID int64, enabled bool, accountIDs ...int64) int64 {
	t.Helper()
	var en int64
	if enabled {
		en = 1
	}
	r, err := q.CreateRule(ctx, dbq.CreateRuleParams{
		Name: name, Pattern: pattern, Direction: direction, SubcategoryID: subID, Enabled: en,
	})
	if err != nil {
		t.Fatalf("CreateRule: %v", err)
	}
	for _, a := range accountIDs {
		if err := q.InsertRuleAccount(ctx, dbq.InsertRuleAccountParams{RuleID: r.ID, AccountID: a}); err != nil {
			t.Fatalf("InsertRuleAccount: %v", err)
		}
	}
	return r.ID
}

func lineSource(t *testing.T, ctx context.Context, q *dbq.Queries, id int64) *dbq.StatementLine {
	t.Helper()
	row, err := q.GetLineForAction(ctx, id)
	if err != nil {
		t.Fatalf("GetLineForAction: %v", err)
	}
	return row
}

func TestCompile(t *testing.T) {
	if _, err := Compile("^NETTO"); err != nil {
		t.Fatalf("valid pattern: %v", err)
	}
	if _, err := Compile("(unclosed"); err == nil {
		t.Fatalf("expected compiler error for invalid regex")
	}
	if _, err := Compile(strings.Repeat("a", MaxPatternLength+1)); err == nil {
		t.Fatalf("expected error for over-long pattern")
	}
	if _, err := Compile(strings.Repeat("a", MaxPatternLength)); err != nil {
		t.Fatalf("max-length pattern should compile: %v", err)
	}
}

func TestMatches(t *testing.T) {
	line := &Line{ID: 1, AccountID: 10, Message: "NETTO, CITY", AmountMinor: -100}

	r := &Rule{Direction: "out", AccountIDs: nil}
	if !Matches(r, mustCompile(t, "NETTO"), line) {
		t.Errorf("expected match")
	}
	// case-sensitive by default
	if Matches(r, mustCompile(t, "netto"), line) {
		t.Errorf("expected case-sensitive non-match")
	}
	if !Matches(r, mustCompile(t, "(?i)netto"), line) {
		t.Errorf("expected (?i) to match")
	}
	// direction
	if Matches(&Rule{Direction: "in"}, mustCompile(t, "NETTO"), line) {
		t.Errorf("amount<0 must not match direction=in")
	}
	if !Matches(&Rule{Direction: "out"}, mustCompile(t, "NETTO"), line) {
		t.Errorf("amount<0 must match direction=out")
	}
	if !Matches(&Rule{Direction: "both"}, mustCompile(t, "NETTO"), line) {
		t.Errorf("direction=both must match any amount")
	}
	// account filter
	if Matches(&Rule{Direction: "both", AccountIDs: []int64{99}}, mustCompile(t, "NETTO"), line) {
		t.Errorf("account 99 filter must not match account 10")
	}
	if !Matches(&Rule{Direction: "both", AccountIDs: []int64{99, 10}}, mustCompile(t, "NETTO"), line) {
		t.Errorf("account filter list must contain account 10")
	}
}

func mustCompile(t *testing.T, p string) *regexp.Regexp {
	t.Helper()
	r, err := Compile(p)
	if err != nil {
		t.Fatalf("Compile(%q): %v", p, err)
	}
	return r
}

func TestEvaluatePrecedence(t *testing.T) {
	ctx, q, acc1, _, groceries, dining := setup(t)

	none1 := addLine(t, ctx, q, acc1, "NETTO STORE", -100, "none")
	rule1ID := addRule(t, ctx, q, "Groceries", `^NETTO`, "out", groceries, true)
	manual1 := addLine(t, ctx, q, acc1, "NETTO MANUAL", -100, "manual")
	transfer1 := addLine(t, ctx, q, acc1, "NETTO TRANSFER", -100, "transfer")
	conflict1 := addLine(t, ctx, q, acc1, "NETTO BOTH", -100, "none")
	addRule(t, ctx, q, "Dining", `NETTO BOTH`, "out", dining, true)

	changes, err := EvaluateAll(ctx, q)
	if err != nil {
		t.Fatalf("EvaluateAll: %v", err)
	}
	// none1 -> rule(groceries), conflict1 -> conflict, manual/transfer untouched
	if got := lineSource(t, ctx, q, none1); got.Source != "rule" || !got.SubcategoryID.Valid || got.SubcategoryID.Int64 != groceries || !got.RuleID.Valid || got.RuleID.Int64 != rule1ID {
		t.Errorf("none1: got source=%s sub=%v rule=%v", got.Source, got.SubcategoryID, got.RuleID)
	}
	if got := lineSource(t, ctx, q, conflict1); got.Source != "conflict" || got.SubcategoryID.Valid || got.RuleID.Valid {
		t.Errorf("conflict1: got source=%s sub=%v rule=%v", got.Source, got.SubcategoryID, got.RuleID)
	}
	if got := lineSource(t, ctx, q, manual1); got.Source != "manual" {
		t.Errorf("manual line must be untouched, got %s", got.Source)
	}
	if got := lineSource(t, ctx, q, transfer1); got.Source != "transfer" {
		t.Errorf("transfer line must be untouched, got %s", got.Source)
	}
	if len(changes) != 2 {
		t.Errorf("expected 2 changes, got %d", len(changes))
	}

	// idempotence: a second run changes nothing
	changes2, err := EvaluateAll(ctx, q)
	if err != nil {
		t.Fatalf("EvaluateAll 2: %v", err)
	}
	if len(changes2) != 0 {
		t.Errorf("expected idempotent second run, got %d changes", len(changes2))
	}

	// conflict resolution honored: resolve conflict1 to the dining rule
	diningRule, err := q.ListRules(ctx, dbq.ListRulesParams{})
	if err != nil {
		t.Fatalf("ListRules: %v", err)
	}
	var diningID int64
	for _, r := range diningRule {
		if r.Name == "Dining" {
			diningID = r.ID
		}
	}
	if _, err := q.UpsertConflictResolution(ctx, dbq.UpsertConflictResolutionParams{LineID: conflict1, RuleID: diningID}); err != nil {
		t.Fatalf("UpsertConflictResolution: %v", err)
	}
	if _, err := EvaluateLines(ctx, q, []int64{conflict1}); err != nil {
		t.Fatalf("EvaluateLines: %v", err)
	}
	if got := lineSource(t, ctx, q, conflict1); got.Source != "rule" || !got.RuleID.Valid || got.RuleID.Int64 != diningID || got.SubcategoryID.Int64 != dining {
		t.Errorf("conflict1 after resolution: got source=%s rule=%v sub=%v", got.Source, got.RuleID, got.SubcategoryID)
	}

	// RULE-9: edit the dining rule so it no longer matches -> resolution dropped, back to conflict
	if _, err := q.UpdateRule(ctx, dbq.UpdateRuleParams{
		Name: "Dining", Pattern: `^NEVER$`, Direction: "out", SubcategoryID: dining, Enabled: 1, ID: diningID,
	}); err != nil {
		t.Fatalf("UpdateRule: %v", err)
	}
	if _, err := EvaluateLines(ctx, q, []int64{conflict1}); err != nil {
		t.Fatalf("EvaluateLines: %v", err)
	}
	if _, err := q.GetConflictResolution(ctx, conflict1); err != sql.ErrNoRows {
		t.Errorf("expected stale conflict_resolution to be deleted, err=%v", err)
	}
	if got := lineSource(t, ctx, q, conflict1); got.Source != "rule" || got.RuleID.Int64 != rule1ID {
		t.Errorf("conflict1 after rule edit: expected rule groceries, got %s rule=%v", got.Source, got.RuleID)
	}
}

func TestEvaluateDisabledRule(t *testing.T) {
	ctx, q, acc1, _, groceries, _ := setup(t)
	lineID := addLine(t, ctx, q, acc1, "NETTO STORE", -100, "none")
	addRule(t, ctx, q, "Off", `^NETTO`, "out", groceries, false)
	if _, err := EvaluateAll(ctx, q); err != nil {
		t.Fatalf("EvaluateAll: %v", err)
	}
	if got := lineSource(t, ctx, q, lineID); got.Source != "none" {
		t.Errorf("disabled rule must not categorize, got %s", got.Source)
	}
}

func TestPreviewGroups(t *testing.T) {
	ctx, q, acc1, acc2, groceries, dining := setup(t)
	addRule(t, ctx, q, "Dining", `DINING`, "both", dining, true)

	willCat := addLine(t, ctx, q, acc1, "NETTO STORE", -100, "none")
	conflict := addLine(t, ctx, q, acc1, "NETTO DINING", -100, "none")
	manual := addLine(t, ctx, q, acc1, "NETTO MANUAL", -100, "manual")
	addLine(t, ctx, q, acc1, "NETTO TR", -100, "transfer")
	other := addLine(t, ctx, q, acc1, " unrelated ", -100, "none")
	_ = willCat

	res, err := Preview(ctx, q, Rule{
		Name: "Groceries", Pattern: `NETTO`, Direction: "out", SubcategoryID: groceries, Enabled: true,
	}, 0)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if res.WillCategorize.Count != 1 || res.WillCategorize.SampleLineIDs[0] != willCat {
		t.Errorf("willCategorize: %+v", res.WillCategorize)
	}
	if res.WillConflict.Count != 1 || res.WillConflict.SampleLineIDs[0] != conflict {
		t.Errorf("willConflict: %+v", res.WillConflict)
	}
	if res.UnaffectedManual.Count != 1 || res.UnaffectedManual.SampleLineIDs[0] != manual {
		t.Errorf("unaffectedManual: %+v", res.UnaffectedManual)
	}
	if res.UnaffectedTransfer.Count != 1 {
		t.Errorf("unaffectedTransfer: %+v", res.UnaffectedTransfer)
	}
	if res.TotalMatches != 4 {
		t.Errorf("totalMatches: expected 4, got %d", res.TotalMatches)
	}
	// the unrelated line must not appear anywhere
	for _, id := range append(append(append(res.WillCategorize.SampleLineIDs, res.WillConflict.SampleLineIDs...), res.UnaffectedManual.SampleLineIDs...), res.UnaffectedTransfer.SampleLineIDs...) {
		if id == other {
			t.Errorf("unrelated line %d must not be in any group", other)
		}
	}

	// invalid pattern -> compiler error (RULE-2)
	if _, err := Preview(ctx, q, Rule{Pattern: "(bad", Direction: "both", Enabled: true}, 0); err == nil {
		t.Errorf("expected compiler error for invalid pattern")
	}

	// disabled def matches nothing
	res2, err := Preview(ctx, q, Rule{Pattern: `NETTO`, Direction: "out", Enabled: false}, 0)
	if err != nil {
		t.Fatalf("Preview disabled: %v", err)
	}
	if res2.TotalMatches != 0 {
		t.Errorf("disabled preview should match nothing, got %d", res2.TotalMatches)
	}

	// account filter: rule limited to acc2 does not see acc1 lines
	res3, err := Preview(ctx, q, Rule{Pattern: `NETTO`, Direction: "out", Enabled: true, AccountIDs: []int64{acc2}}, 0)
	if err != nil {
		t.Fatalf("Preview acc filter: %v", err)
	}
	if res3.TotalMatches != 0 {
		t.Errorf("account-filtered preview should match 0 acc1 lines, got %d", res3.TotalMatches)
	}
}

func TestDiff(t *testing.T) {
	ctx, q, acc1, _, groceries, _ := setup(t)
	ruleID := addRule(t, ctx, q, "Shops", `^NETTO|^BILKA`, "out", groceries, true)

	a := addLine(t, ctx, q, acc1, "NETTO STORE", -100, "none")
	b := addLine(t, ctx, q, acc1, "BILKA STORE", -100, "none")
	c := addLine(t, ctx, q, acc1, "OTHER SHOP", -100, "none")

	// narrow the pattern to NETTO only: BILKA line is removed, OTHER untouched...
	// wait: old pattern matched a+b; new matches a only -> removed=[b], unchanged=[a], added=[]
	diff, err := Diff(ctx, q, ruleID, Rule{
		Name: "Shops", Pattern: `^NETTO`, Direction: "out", SubcategoryID: groceries, Enabled: true,
	})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(diff.Added) != 0 || len(diff.Removed) != 1 || diff.Removed[0] != b || len(diff.Unchanged) != 1 || diff.Unchanged[0] != a {
		t.Errorf("diff sets wrong: %+v", diff)
	}
	if diff.RemovedFutureTotal != 1 || len(diff.RemovedFuture) != 1 || diff.RemovedFuture[0].LineID != b || diff.RemovedFuture[0].Future != "none" {
		t.Errorf("removedFuture wrong: %+v", diff.RemovedFuture)
	}

	// widen: old stored=^NETTO|^BILKA, new=^(NETTO|OTHER) -> added=[c], removed=[b], unchanged=[a]
	diff2, err := Diff(ctx, q, ruleID, Rule{
		Name: "Shops", Pattern: `^(NETTO|OTHER)`, Direction: "out", SubcategoryID: groceries, Enabled: true,
	})
	if err != nil {
		t.Fatalf("Diff2: %v", err)
	}
	if len(diff2.Added) != 1 || diff2.Added[0] != c || len(diff2.Removed) != 1 || diff2.Removed[0] != b || len(diff2.Unchanged) != 1 || diff2.Unchanged[0] != a {
		t.Errorf("diff2 sets wrong: %+v", diff2)
	}

	// removed line that falls back to another rule: add a rule matching OTHER
	addRule(t, ctx, q, "Other", `^OTHER`, "out", groceries, true)
	diff3, err := Diff(ctx, q, ruleID, Rule{
		Name: "Shops", Pattern: `^NETTO`, Direction: "out", SubcategoryID: groceries, Enabled: true,
	})
	if err != nil {
		t.Fatalf("Diff3: %v", err)
	}
	// old (stored) is ^NETTO|^BILKA; new ^NETTO -> removed=[b]; b matches no other rule -> none
	if diff3.RemovedFuture[0].Future != "none" {
		t.Errorf("expected future none, got %+v", diff3.RemovedFuture[0])
	}
	// now remove c from the new pattern too by diffing against a pattern matching nothing extra:
	diff4, err := Diff(ctx, q, ruleID, Rule{
		Name: "Shops", Pattern: `^BILKA`, Direction: "out", SubcategoryID: groceries, Enabled: true,
	})
	if err != nil {
		t.Fatalf("Diff4: %v", err)
	}
	// old=^NETTO|^BILKA, new=^BILKA -> removed=[a]; a matches no other rule -> none
	var foundA bool
	for _, rf := range diff4.RemovedFuture {
		if rf.LineID == a {
			foundA = true
			if rf.Future != "none" {
				t.Errorf("a future: expected none, got %+v", rf)
			}
		}
	}
	if !foundA {
		t.Errorf("expected a in removedFuture: %+v", diff4)
	}
}
