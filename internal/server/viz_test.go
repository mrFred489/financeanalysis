package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mrFred489/financeanalysis/internal/db"
	"github.com/mrFred489/financeanalysis/internal/db/dbq"
)

// M5 integration tests (plan docs/plans/M5-visualization.md step 3/8): the viz
// endpoints over httptest + temp SQLite, with fixture lines covering spread,
// transfers, conflicts and filters.

// setupVizDB creates a test server + database with one account, a few
// subcategories and some statement lines inserted directly.
func setupVizDB(t *testing.T) (*httptest.Server, int64, map[string]int64) {
	t.Helper()
	ts := newTestServer(t)
	accID := createDBViaAPI(t, ts)

	d, err := db.GetDB()
	if err != nil {
		t.Fatalf("GetDB: %v", err)
	}
	q := dbq.New(d)
	ctx := t.Context()

	// createDBViaAPI already creates "Groceries" under variable (category 3).
	// Look it up, then add Salary (income=1) and Rent (fixed=2).
	subs, err := q.ListSubcategories(ctx)
	if err != nil {
		t.Fatalf("ListSubcategories: %v", err)
	}
	subIDs := map[string]int64{}
	for _, s := range subs {
		subIDs[s.Name] = s.ID
	}
	for _, s := range []struct {
		name string
		cat  int64
	}{
		{"Salary", 1}, {"Rent", 2},
	} {
		sub, err := q.CreateSubcategory(ctx, dbq.CreateSubcategoryParams{
			CategoryID: s.cat, Name: s.name, Color: "#6c5ce7", SortOrder: 1,
		})
		if err != nil {
			t.Fatalf("CreateSubcategory %s: %v", s.name, err)
		}
		subIDs[s.name] = sub.ID
	}

	insert := func(date, msg string, amount int64, subID *int64, source string, spread int64) {
		t.Helper()
		var sub sql.NullInt64
		if subID != nil {
			sub = sql.NullInt64{Int64: *subID, Valid: true}
		}
		_, err := q.InsertStatementLine(ctx, dbq.InsertStatementLineParams{
			AccountID: accID, BookingDate: date, Message: msg,
			AmountMinor: amount, Currency: "DKK", FileRow: 1, Seq: 1, Occurrence: 1,
		})
		if err != nil {
			t.Fatalf("InsertStatementLine: %v", err)
		}
		// set source/subcategory/spread via updates
		if source != "none" || subID != nil || spread != 1 {
			if _, err := d.ExecContext(ctx,
				`UPDATE statement_line SET source=?, subcategory_id=?, spread_months=? WHERE booking_date=? AND message=? AND amount_minor=?`,
				source, sub, spread, date, msg, amount); err != nil {
				t.Fatalf("update line: %v", err)
			}
		}
	}

	groc := subIDs["Groceries"]
	sal := subIDs["Salary"]
	rent := subIDs["Rent"]
	insert("2026-01-15", "GROCERY STORE", -48514, &groc, "manual", 1)
	insert("2026-01-01", "SALARY", 3000000, &sal, "rule", 1)
	insert("2026-01-05", "LANDLORD", -1200000, &rent, "rule", 1)
	insert("2026-02-10", "BIG PURCHASE", -30000, &groc, "manual", 3) // spread over Feb/Mar/Apr
	insert("2026-02-20", "UNCATEGORIZED THING", -500, nil, "none", 1)
	insert("2026-02-21", "CONFLICTED", -200, nil, "conflict", 1)

	return ts, accID, subIDs
}

type vizBucket struct {
	Month         string           `json:"month"`
	ByCategory    map[string]int64 `json:"byCategory"`
	BySubcategory map[string]int64 `json:"bySubcategory"` // JSON keys are strings
	ByAccount     map[string]int64 `json:"byAccount"`
	Uncategorized int64            `json:"uncategorized"`
	Conflict      int64            `json:"conflict"`
	Transfer      int64            `json:"transfer"`
}

func getTimeseries(t *testing.T, ts *httptest.Server, query string) []vizBucket {
	t.Helper()
	status, _, raw := doJSON(t, ts, "GET", "/api/viz/timeseries"+query, nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/viz/timeseries%s: %d %s", query, status, raw)
	}
	var resp struct {
		Buckets []vizBucket `json:"buckets"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode timeseries: %v (%s)", err, raw)
	}
	return resp.Buckets
}

func TestVizTimeseriesBasic(t *testing.T) {
	ts, _, subIDs := setupVizDB(t)
	buckets := getTimeseries(t, ts, "")

	// months: 2026-01, 2026-02, 2026-03, 2026-04 (spread from Feb purchase)
	if len(buckets) != 4 {
		t.Fatalf("expected 4 buckets, got %d: %+v", len(buckets), buckets)
	}
	jan := buckets[0]
	if jan.Month != "2026-01" {
		t.Errorf("first bucket = %s, want 2026-01", jan.Month)
	}
	if jan.ByCategory["income"] != 3000000 {
		t.Errorf("jan income = %d, want 3000000", jan.ByCategory["income"])
	}
	if jan.ByCategory["fixed"] != -1200000 {
		t.Errorf("jan fixed = %d, want -1200000", jan.ByCategory["fixed"])
	}
	if jan.ByCategory["variable"] != -48514 {
		t.Errorf("jan variable = %d, want -48514", jan.ByCategory["variable"])
	}
	feb := buckets[1]
	// Feb: spread first month = -30000/3 + remainder = -10000 + 0 = -10000, plus uncategorized -500, conflict -200
	if feb.BySubcategory[fmt.Sprint(subIDs["Groceries"])] != -10000 {
		t.Errorf("feb groceries = %d, want -10000", feb.BySubcategory[fmt.Sprint(subIDs["Groceries"])])
	}
	if feb.Uncategorized != -500 {
		t.Errorf("feb uncategorized = %d, want -500", feb.Uncategorized)
	}
	if feb.Conflict != -200 {
		t.Errorf("feb conflict = %d, want -200", feb.Conflict)
	}
	// Mar and Apr get the spread remainder
	mar := buckets[2]
	if mar.BySubcategory[fmt.Sprint(subIDs["Groceries"])] != -10000 {
		t.Errorf("mar groceries = %d, want -10000", mar.BySubcategory[fmt.Sprint(subIDs["Groceries"])])
	}
}

func TestVizTimeseriesDateFilter(t *testing.T) {
	ts, _, _ := setupVizDB(t)
	// Filter to Jan only: the Feb spread line should NOT appear at all,
	// and Jan lines should be present.
	buckets := getTimeseries(t, ts, "?from=2026-01-01&to=2026-01-31")
	if len(buckets) != 1 {
		t.Fatalf("expected 1 bucket for Jan only, got %d: %+v", len(buckets), buckets)
	}
	if buckets[0].Month != "2026-01" {
		t.Errorf("bucket = %s, want 2026-01", buckets[0].Month)
	}
	if buckets[0].ByCategory["income"] != 3000000 {
		t.Errorf("jan income = %d, want 3000000", buckets[0].ByCategory["income"])
	}
	// Feb filter: the spread line booked in Feb creates Feb/Mar/Apr buckets
	buckets = getTimeseries(t, ts, "?from=2026-02-01&to=2026-02-28")
	if len(buckets) != 3 {
		t.Fatalf("expected 3 buckets (Feb+spread Mar/Apr), got %d", len(buckets))
	}
	for _, b := range buckets {
		if b.ByCategory["income"] != 0 {
			t.Errorf("income should be 0 in Feb-filtered set, got %d in %s", b.ByCategory["income"], b.Month)
		}
	}
}

func TestVizTimeseriesCategoryFilter(t *testing.T) {
	ts, _, _ := setupVizDB(t)
	// category 1 = income
	buckets := getTimeseries(t, ts, "?categories=1")
	for _, b := range buckets {
		if b.ByCategory["fixed"] != 0 || b.ByCategory["variable"] != 0 {
			t.Errorf("non-income category present: %+v", b)
		}
	}
	// should have income in Jan
	found := false
	for _, b := range buckets {
		if b.ByCategory["income"] == 3000000 {
			found = true
		}
	}
	if !found {
		t.Error("income 3000000 not found with categories=1 filter")
	}
}

func TestVizTimeseriesEmptyDB(t *testing.T) {
	ts := newTestServer(t)
	createDBViaAPI(t, ts)
	buckets := getTimeseries(t, ts, "")
	if len(buckets) != 0 {
		t.Errorf("expected 0 buckets for empty DB, got %d", len(buckets))
	}
}

type vizShare struct {
	SubcategoryID   int64  `json:"subcategoryId"`
	SubcategoryName string `json:"subcategoryName"`
	CategoryKey     string `json:"categoryKey"`
	Color           string `json:"color"`
	AmountMinor     int64  `json:"amountMinor"`
}

func getShare(t *testing.T, ts *httptest.Server, query string) []vizShare {
	t.Helper()
	status, _, raw := doJSON(t, ts, "GET", "/api/viz/share"+query, nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/viz/share%s: %d %s", query, status, raw)
	}
	var resp struct {
		Shares []vizShare `json:"shares"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode share: %v (%s)", err, raw)
	}
	return resp.Shares
}

func TestVizShare(t *testing.T) {
	ts, _, subIDs := setupVizDB(t)
	shares := getShare(t, ts, "")
	// Should have Groceries (-48514 + -30000 = -78514), Salary (3000000), Rent (-1200000)
	if len(shares) != 3 {
		t.Fatalf("expected 3 shares, got %d: %+v", len(shares), shares)
	}
	byID := map[int64]vizShare{}
	for _, s := range shares {
		byID[s.SubcategoryID] = s
	}
	if byID[subIDs["Groceries"]].AmountMinor != -78514 {
		t.Errorf("groceries share = %d, want -78514", byID[subIDs["Groceries"]].AmountMinor)
	}
	if byID[subIDs["Salary"]].AmountMinor != 3000000 {
		t.Errorf("salary share = %d, want 3000000", byID[subIDs["Salary"]].AmountMinor)
	}
	if byID[subIDs["Groceries"]].SubcategoryName != "Groceries" {
		t.Errorf("groceries name = %q", byID[subIDs["Groceries"]].SubcategoryName)
	}
	if byID[subIDs["Salary"]].CategoryKey != "income" {
		t.Errorf("salary category = %q, want income", byID[subIDs["Salary"]].CategoryKey)
	}
}

func TestVizShareEmpty(t *testing.T) {
	ts := newTestServer(t)
	createDBViaAPI(t, ts)
	shares := getShare(t, ts, "")
	if len(shares) != 0 {
		t.Errorf("expected 0 shares, got %d", len(shares))
	}
}

func TestVizTimeseriesIncludeTransfers(t *testing.T) {
	ts, accID, _ := setupVizDB(t)
	// Add a transfer pair manually via direct DB insert
	d, err := db.GetDB()
	if err != nil {
		t.Fatalf("GetDB: %v", err)
	}
	ctx := t.Context()
	q := dbq.New(d)
	_, err = q.InsertStatementLine(ctx, dbq.InsertStatementLineParams{
		AccountID: accID, BookingDate: "2026-03-10", Message: "TRANSFER OUT",
		AmountMinor: -5000, Currency: "DKK", FileRow: 1, Seq: 2, Occurrence: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.ExecContext(ctx, `UPDATE statement_line SET source='transfer' WHERE message='TRANSFER OUT'`)
	if err != nil {
		t.Fatal(err)
	}

	// Default: transfers excluded
	buckets := getTimeseries(t, ts, "?from=2026-03-01&to=2026-03-31")
	for _, b := range buckets {
		if b.Transfer != 0 {
			t.Errorf("transfer should be 0 by default, got %d", b.Transfer)
		}
	}
	// With includeTransfers=1: transfer series appears
	buckets = getTimeseries(t, ts, "?from=2026-03-01&to=2026-03-31&includeTransfers=1")
	found := false
	for _, b := range buckets {
		if b.Transfer == -5000 {
			found = true
		}
	}
	if !found {
		t.Error("transfer -5000 not found with includeTransfers=1")
	}
}
