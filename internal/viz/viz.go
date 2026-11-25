// Package viz implements M5 visualization aggregation (PRD section 7.9, plan
// docs/plans/M5-visualization.md D1/D5/D6): fetch filtered statement lines via
// SQL, then aggregate in Go into monthly buckets with spread applied, and into
// per-subcategory shares for the donut chart.
//
// Aggregation is pure Go over a SQL-fetched line set (same pattern as M3 rules
// and M4 transfers), not complex SQL GROUP BY, because spread (distributing a
// line's amount over X months) is much easier to get right and test in Go.
package viz

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/mrFred489/financeanalysis/internal/db/dbq"
)

// Filters are the global viz filter params (VIZ-2). Empty slices mean "all".
type Filters struct {
	From             string
	To               string
	AccountIDs       []int64
	CategoryIDs      []int64
	SubcategoryIDs   []int64
	IncludeTransfers bool
}

// Line is one minimal statement line for aggregation.
type Line struct {
	ID            int64
	AccountID     int64
	BookingDate   string // YYYY-MM-DD
	AmountMinor   int64
	SubcategoryID sql.NullInt64
	Source        string
	SpreadMonths  int64
	CategoryKey   sql.NullString // from the JOIN; empty for uncategorized lines
}

// MonthBucket is one calendar month's aggregated totals (minor units, signed).
type MonthBucket struct {
	Month         string           `json:"month"` // YYYY-MM
	ByCategory    map[string]int64 `json:"byCategory"`
	BySubcategory map[int64]int64  `json:"bySubcategory"`
	ByAccount     map[int64]int64  `json:"byAccount"`
	Uncategorized int64            `json:"uncategorized"`
	Conflict      int64            `json:"conflict"`
	Transfer      int64            `json:"transfer"`
}

// SubcategoryShare is one slice of the donut/treemap (VIZ-1d). AmountMinor is
// the raw (unspread) sum of lines booked in the selected range.
type SubcategoryShare struct {
	SubcategoryID   int64  `json:"subcategoryId"`
	SubcategoryName string `json:"subcategoryName"`
	CategoryKey     string `json:"categoryKey"`
	Color           string `json:"color"`
	AmountMinor     int64  `json:"amountMinor"`
}

// categoryKeys are the fixed top-level category keys (PRD section 7.3 CAT-1).
var categoryKeys = map[string]bool{
	"income": true, "fixed": true, "variable": true, "savings": true,
}

// FetchLines loads the filtered minimal line set from the database (D3).
func FetchLines(ctx context.Context, q *dbq.Queries, f Filters) ([]Line, error) {
	accounts := idListJSON(f.AccountIDs)
	categories := idListJSON(f.CategoryIDs)
	subcategories := idListJSON(f.SubcategoryIDs)
	includeTransfers := int64(0)
	if f.IncludeTransfers {
		includeTransfers = 1
	}
	rows, err := q.ListLinesForViz(ctx, dbq.ListLinesForVizParams{
		FromDate:         nullStr(f.From),
		ToDate:           nullStr(f.To),
		Accounts:         accounts,
		Categories:       categories,
		Subcategories:    subcategories,
		IncludeTransfers: includeTransfers,
	})
	if err != nil {
		return nil, fmt.Errorf("list lines for viz: %w", err)
	}
	lines := make([]Line, 0, len(rows))
	for _, r := range rows {
		lines = append(lines, Line{
			ID:            r.ID,
			AccountID:     r.AccountID,
			BookingDate:   r.BookingDate,
			AmountMinor:   r.AmountMinor,
			SubcategoryID: r.SubcategoryID,
			Source:        r.Source,
			SpreadMonths:  r.SpreadMonths,
			CategoryKey:   r.CategoryKey,
		})
	}
	return lines, nil
}

// Aggregate groups lines into monthly buckets, applying spread (D5) and the
// net-sum-per-category semantics (D6). Returns buckets sorted by month ascending.
func Aggregate(lines []Line) []MonthBucket {
	byMonth := map[string]*MonthBucket{}
	for _, ln := range lines {
		month, err := monthOf(ln.BookingDate)
		if err != nil {
			continue // unparseable date: skip
		}
		spreadMonths := ln.SpreadMonths
		if spreadMonths < 1 {
			spreadMonths = 1
		}
		base := ln.AmountMinor / spreadMonths
		rem := ln.AmountMinor - base*spreadMonths // remainder goes to the first month
		for i := int64(0); i < spreadMonths; i++ {
			m := addMonths(month, int(i))
			b := byMonth[m]
			if b == nil {
				b = &MonthBucket{
					Month:         m,
					ByCategory:    map[string]int64{},
					BySubcategory: map[int64]int64{},
					ByAccount:     map[int64]int64{},
				}
				byMonth[m] = b
			}
			amt := base
			if i == 0 {
				amt += rem
			}
			b.ByAccount[ln.AccountID] += amt
			switch ln.Source {
			case "transfer":
				b.Transfer += amt
			case "conflict":
				b.Conflict += amt
			default: // none, rule, manual
				if ln.SubcategoryID.Valid && ln.CategoryKey.Valid && categoryKeys[ln.CategoryKey.String] {
					b.ByCategory[ln.CategoryKey.String] += amt
					b.BySubcategory[ln.SubcategoryID.Int64] += amt
				} else {
					b.Uncategorized += amt
				}
			}
		}
	}
	buckets := make([]MonthBucket, 0, len(byMonth))
	for _, b := range byMonth {
		buckets = append(buckets, *b)
	}
	sort.Slice(buckets, func(i, j int) bool { return buckets[i].Month < buckets[j].Month })
	return buckets
}

// Share computes per-subcategory sums for the donut/treemap (VIZ-1d). Uses raw
// (unspread) amounts for lines booked in the fetched range (D2 note: spreading
// would double-count across months outside the range).
func Share(lines []Line, subcatMeta map[int64]SubcatMeta) []SubcategoryShare {
	sums := map[int64]int64{}
	for _, ln := range lines {
		if ln.Source == "transfer" || ln.Source == "conflict" {
			continue
		}
		if !ln.SubcategoryID.Valid {
			continue
		}
		sums[ln.SubcategoryID.Int64] += ln.AmountMinor
	}
	shares := make([]SubcategoryShare, 0, len(sums))
	for id, amt := range sums {
		meta := subcatMeta[id]
		shares = append(shares, SubcategoryShare{
			SubcategoryID:   id,
			SubcategoryName: meta.Name,
			CategoryKey:     meta.CategoryKey,
			Color:           meta.Color,
			AmountMinor:     amt,
		})
	}
	sort.Slice(shares, func(i, j int) bool {
		if shares[i].AmountMinor != shares[j].AmountMinor {
			return shares[i].AmountMinor < shares[j].AmountMinor // ascending; frontend reverses
		}
		return shares[i].SubcategoryID < shares[j].SubcategoryID
	})
	return shares
}

// SubcatMeta carries display metadata for a subcategory (name, category, color).
type SubcatMeta struct {
	Name        string
	CategoryKey string
	Color       string
}

// monthOf extracts the YYYY-MM prefix from a YYYY-MM-DD date.
func monthOf(date string) (string, error) {
	if len(date) < 7 {
		return "", fmt.Errorf("bad date %q", date)
	}
	// validate it parses
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return "", err
	}
	return date[:7], nil
}

// addMonths returns the YYYY-MM string n calendar months after the given YYYY-MM.
func addMonths(month string, n int) string {
	var y, m int
	fmt.Sscanf(month, "%d-%d", &y, &m)
	t := time.Date(y, time.Month(m), 1, 0, 0, 0, 0, time.UTC)
	t = t.AddDate(0, n, 0)
	return fmt.Sprintf("%04d-%02d", t.Year(), int(t.Month()))
}

// nullStr returns nil for empty strings (SQL NULL disables the filter).
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// idListJSON serializes an int64 slice to a JSON array string for json_each(),
// or nil when empty (disables the filter).
func idListJSON(ids []int64) any {
	if len(ids) == 0 {
		return nil
	}
	return fmt.Sprintf(`[%s]`, joinIDs(ids))
}

func joinIDs(ids []int64) string {
	s := ""
	for i, id := range ids {
		if i > 0 {
			s += ","
		}
		s += fmt.Sprintf("%d", id)
	}
	return s
}
