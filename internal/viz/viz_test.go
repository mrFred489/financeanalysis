package viz

import (
	"database/sql"
	"testing"
)

func line(id, accountID int64, date string, amount int64, source string, spread int64, subcatID int64, catKey string) Line {
	l := Line{
		ID: id, AccountID: accountID, BookingDate: date, AmountMinor: amount,
		Source: source, SpreadMonths: spread,
	}
	if subcatID > 0 {
		l.SubcategoryID = sql.NullInt64{Int64: subcatID, Valid: true}
	}
	if catKey != "" {
		l.CategoryKey = sql.NullString{String: catKey, Valid: true}
	}
	return l
}

func TestMonthOf(t *testing.T) {
	m, err := monthOf("2026-03-15")
	if err != nil || m != "2026-03" {
		t.Fatalf("monthOf = %q, %v", m, err)
	}
	if _, err := monthOf("bad"); err == nil {
		t.Fatal("expected error for bad date")
	}
}

func TestAddMonths(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"2026-01", 0, "2026-01"},
		{"2026-12", 1, "2027-01"},
		{"2026-11", 3, "2027-02"},
		{"2024-02", 12, "2025-02"}, // leap year
		{"2026-06", 36, "2029-06"},
	}
	for _, c := range cases {
		if got := addMonths(c.in, c.n); got != c.want {
			t.Errorf("addMonths(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}

func TestAggregateSpreadRemainderToFirstMonth(t *testing.T) {
	// 100 øre spread over 3 months: base=33, rem=1 → first month gets 34, then 33, 33
	lines := []Line{
		line(1, 1, "2026-01-10", 100, "manual", 3, 10, "variable"),
	}
	buckets := Aggregate(lines)
	if len(buckets) != 3 {
		t.Fatalf("expected 3 buckets, got %d", len(buckets))
	}
	if buckets[0].Month != "2026-01" || buckets[0].BySubcategory[10] != 34 {
		t.Errorf("first month: %s subcat=%d, want 2026-01 34", buckets[0].Month, buckets[0].BySubcategory[10])
	}
	if buckets[1].Month != "2026-02" || buckets[1].BySubcategory[10] != 33 {
		t.Errorf("second month: %s subcat=%d, want 2026-02 33", buckets[1].Month, buckets[1].BySubcategory[10])
	}
	if buckets[2].Month != "2026-03" || buckets[2].BySubcategory[10] != 33 {
		t.Errorf("third month: %s subcat=%d, want 2026-03 33", buckets[2].Month, buckets[2].BySubcategory[10])
	}
}

func TestAggregateSpreadNegativeAmount(t *testing.T) {
	// -485,14 kr = -48514 øre over 2 months: base=-24257, rem=0 → -24257, -24257
	lines := []Line{
		line(1, 1, "2026-05-05", -48514, "rule", 2, 10, "variable"),
	}
	buckets := Aggregate(lines)
	if len(buckets) != 2 {
		t.Fatalf("expected 2 buckets, got %d", len(buckets))
	}
	if buckets[0].ByCategory["variable"] != -24257 || buckets[1].ByCategory["variable"] != -24257 {
		t.Errorf("got %d / %d, want -24257 / -24257", buckets[0].ByCategory["variable"], buckets[1].ByCategory["variable"])
	}
}

func TestAggregateSpreadAcrossYearBoundary(t *testing.T) {
	lines := []Line{
		line(1, 1, "2026-11-15", 300, "manual", 4, 10, "fixed"), // Nov, Dec, Jan, Feb
	}
	buckets := Aggregate(lines)
	if len(buckets) != 4 {
		t.Fatalf("expected 4 buckets, got %d: %+v", len(buckets), buckets)
	}
	wantMonths := []string{"2026-11", "2026-12", "2027-01", "2027-02"}
	for i, w := range wantMonths {
		if buckets[i].Month != w {
			t.Errorf("bucket %d: %s, want %s", i, buckets[i].Month, w)
		}
		if buckets[i].ByCategory["fixed"] != 75 {
			t.Errorf("bucket %d fixed: %d, want 75", i, buckets[i].ByCategory["fixed"])
		}
	}
}

func TestAggregateUncategorizedAndConflict(t *testing.T) {
	lines := []Line{
		line(1, 1, "2026-03-01", -500, "none", 1, 0, ""),
		line(2, 1, "2026-03-02", -200, "conflict", 1, 0, ""),
		line(3, 1, "2026-03-03", 10000, "manual", 1, 10, "income"),
	}
	buckets := Aggregate(lines)
	if len(buckets) != 1 {
		t.Fatalf("expected 1 bucket, got %d", len(buckets))
	}
	b := buckets[0]
	if b.Uncategorized != -500 {
		t.Errorf("uncategorized = %d, want -500", b.Uncategorized)
	}
	if b.Conflict != -200 {
		t.Errorf("conflict = %d, want -200", b.Conflict)
	}
	if b.ByCategory["income"] != 10000 {
		t.Errorf("income = %d, want 10000", b.ByCategory["income"])
	}
}

func TestAggregateTransferSeries(t *testing.T) {
	lines := []Line{
		line(1, 1, "2026-03-01", -5000, "transfer", 1, 0, ""),
		line(2, 2, "2026-03-01", 5000, "transfer", 1, 0, ""),
	}
	buckets := Aggregate(lines)
	if len(buckets) != 1 {
		t.Fatalf("expected 1 bucket, got %d", len(buckets))
	}
	if buckets[0].Transfer != 0 {
		t.Errorf("transfer net = %d, want 0 (out + in)", buckets[0].Transfer)
	}
	if buckets[0].ByAccount[1] != -5000 || buckets[0].ByAccount[2] != 5000 {
		t.Errorf("byAccount = %v", buckets[0].ByAccount)
	}
}

func TestAggregateEmpty(t *testing.T) {
	buckets := Aggregate(nil)
	if len(buckets) != 0 {
		t.Errorf("expected 0 buckets, got %d", len(buckets))
	}
}

func TestAggregateSortedByMonth(t *testing.T) {
	lines := []Line{
		line(1, 1, "2026-05-01", -100, "none", 1, 0, ""),
		line(2, 1, "2026-01-01", -100, "none", 1, 0, ""),
		line(3, 1, "2026-03-01", -100, "none", 1, 0, ""),
	}
	buckets := Aggregate(lines)
	if len(buckets) != 3 {
		t.Fatalf("expected 3 buckets, got %d", len(buckets))
	}
	if buckets[0].Month != "2026-01" || buckets[1].Month != "2026-03" || buckets[2].Month != "2026-05" {
		t.Errorf("not sorted: %s %s %s", buckets[0].Month, buckets[1].Month, buckets[2].Month)
	}
}

func TestShare(t *testing.T) {
	lines := []Line{
		line(1, 1, "2026-03-01", -500, "manual", 1, 10, "variable"),
		line(2, 1, "2026-03-02", -300, "rule", 1, 10, "variable"),
		line(3, 1, "2026-03-03", 10000, "manual", 1, 20, "income"),
		line(4, 1, "2026-03-04", -200, "transfer", 1, 0, ""), // excluded
		line(5, 1, "2026-03-05", -100, "conflict", 1, 0, ""), // excluded
	}
	meta := map[int64]SubcatMeta{
		10: {Name: "Groceries", CategoryKey: "variable", Color: "#6c5ce7"},
		20: {Name: "Salary", CategoryKey: "income", Color: "#00b894"},
	}
	shares := Share(lines, meta)
	if len(shares) != 2 {
		t.Fatalf("expected 2 shares, got %d", len(shares))
	}
	// ascending by amount: -800 (groceries) then 10000 (salary)
	if shares[0].SubcategoryID != 10 || shares[0].AmountMinor != -800 {
		t.Errorf("share[0] = %+v", shares[0])
	}
	if shares[1].SubcategoryID != 20 || shares[1].AmountMinor != 10000 {
		t.Errorf("share[1] = %+v", shares[1])
	}
	if shares[0].SubcategoryName != "Groceries" || shares[0].Color != "#6c5ce7" {
		t.Errorf("share[0] meta wrong: %+v", shares[0])
	}
}

func TestShareEmpty(t *testing.T) {
	shares := Share(nil, nil)
	if len(shares) != 0 {
		t.Errorf("expected 0 shares, got %d", len(shares))
	}
}

func TestAggregateBenchmark40k(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping benchmark in short mode")
	}
	lines := make([]Line, 40000)
	for i := range lines {
		lines[i] = Line{
			ID: int64(i + 1), AccountID: int64(i%3 + 1),
			BookingDate:   "2024-01-01",
			AmountMinor:   int64(i%1000) - 500,
			Source:        "manual",
			SpreadMonths:  1,
			SubcategoryID: sql.NullInt64{Int64: int64(i%20 + 1), Valid: true},
			CategoryKey:   sql.NullString{String: "variable", Valid: true},
		}
		// spread the dates over 60 months
		m := i % 60
		lines[i].BookingDate = addMonths("2022-01", m) + "-15"
	}
	buckets := Aggregate(lines)
	if len(buckets) != 60 {
		t.Errorf("expected 60 buckets, got %d", len(buckets))
	}
}
