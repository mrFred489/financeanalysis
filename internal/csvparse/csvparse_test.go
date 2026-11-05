package csvparse

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "fixtures", "csv", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read fixture %s: %v", name, err)
	}
	return string(data)
}

func TestParseNormal(t *testing.T) {
	res, err := Parse(strings.NewReader(fixture(t, "normal.csv")))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(res.Lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(res.Lines))
	}
	if len(res.Errors) != 0 || len(res.Warnings) != 0 {
		t.Errorf("expected no errors/warnings, got %v / %v", res.Errors, res.Warnings)
	}

	// File is newest-first on the booking date (column 8); seq is the reverse (chronological, IMP-8a)
	first := res.Lines[0]
	if first.Date != "2024-12-31" {
		t.Errorf("expected date 2024-12-31, got %q", first.Date)
	}
	if first.Message != "Løn december" {
		t.Errorf("expected message %q, got %q", "Løn december", first.Message)
	}
	if first.AmountMinor != 2500000 {
		t.Errorf("expected amount 2500000, got %d", first.AmountMinor)
	}
	if first.BalanceMinor != nil {
		t.Errorf("expected nil balance (no balance column in v2 export), got %v", *first.BalanceMinor)
	}
	if first.AccountFrom != "9999 0000000001" {
		t.Errorf("expected account 9999 0000000001, got %q", first.AccountFrom)
	}
	if first.BankRef != "0000000001" {
		t.Errorf("expected bank ref 0000000001, got %q", first.BankRef)
	}
	if first.Currency != "DKK" {
		t.Errorf("expected default currency DKK, got %q", first.Currency)
	}
	if first.Seq != 3 {
		t.Errorf("expected seq 3 for newest row, got %d", first.Seq)
	}
	if res.Lines[2].Seq != 1 {
		t.Errorf("expected seq 1 for oldest row, got %d", res.Lines[2].Seq)
	}

	second := res.Lines[1]
	if second.AmountMinor != -135797 {
		t.Errorf("expected -135797 for -1.357,97, got %d", second.AmountMinor)
	}
	if second.FileRow != 2 {
		t.Errorf("expected file_row 2, got %d", second.FileRow)
	}
}

func TestParseBOMSemicolonAndCRLF(t *testing.T) {
	res, err := Parse(strings.NewReader(fixture(t, "bom_semicolon.csv")))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(res.Lines) != 3 {
		t.Fatalf("expected 3 lines, got %d (errors: %v)", len(res.Lines), res.Errors)
	}
	// BOM must be stripped, quoted ';' kept inside the message
	if res.Lines[0].Date != "2025-01-15" {
		t.Errorf("expected date 2025-01-15 (BOM stripped), got %q", res.Lines[0].Date)
	}
	if res.Lines[0].Message != "Betaling; kort" {
		t.Errorf("expected message %q, got %q", "Betaling; kort", res.Lines[0].Message)
	}
	if res.Lines[1].Message != `He said "hi"` {
		t.Errorf("expected quoted message preserved, got %q", res.Lines[1].Message)
	}
	// Unquoted ';' in the message: surplus fields are joined back with ';'
	if res.Lines[2].Message != "Køb; a; b" {
		t.Errorf("expected message %q, got %q", "Køb; a; b", res.Lines[2].Message)
	}
	if res.Lines[2].AmountMinor != -1000 {
		t.Errorf("expected -1000, got %d", res.Lines[2].AmountMinor)
	}
	if res.Lines[2].BankRef != "0000000012" {
		t.Errorf("expected bank ref 0000000012 despite the extra fields, got %q", res.Lines[2].BankRef)
	}
}

func TestParseErrors(t *testing.T) {
	res, err := Parse(strings.NewReader(fixture(t, "parse_errors.csv")))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(res.Lines) != 2 {
		t.Errorf("expected 2 valid lines, got %d", len(res.Lines))
	}
	if len(res.Errors) != 5 {
		t.Fatalf("expected 5 parse errors, got %d: %v", len(res.Errors), res.Errors)
	}
	rows := map[int]string{}
	for _, e := range res.Errors {
		rows[e.Row] = e.Reason
	}
	for _, want := range []int{2, 3, 4, 5, 6} {
		if _, ok := rows[want]; !ok {
			t.Errorf("expected parse error on row %d, got rows %v", want, rows)
		}
	}
}

func TestParseNonDKK(t *testing.T) {
	res, err := Parse(strings.NewReader(fixture(t, "non_dkk.csv")))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(res.Lines) != 1 {
		t.Errorf("expected 1 DKK line, got %d", len(res.Lines))
	}
	if len(res.Warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %d: %v", len(res.Warnings), res.Warnings)
	}
	if res.Warnings[0].Row != 2 || res.Warnings[1].Row != 3 {
		t.Errorf("expected warnings on rows 2 and 3, got %v", res.Warnings)
	}
}

func TestParseDuplicateOccurrence(t *testing.T) {
	res, err := Parse(strings.NewReader(fixture(t, "duplicates.csv")))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(res.Lines) != 4 {
		t.Fatalf("expected 4 lines, got %d", len(res.Lines))
	}
	// The occurrence key has no balance (PRD section 6): rows 1-3 are identical
	// tuples -> occurrence 1, 2, 3; row 4 differs -> occurrence 1
	want := []int{1, 2, 3, 1}
	for i, w := range want {
		if res.Lines[i].Occurrence != w {
			t.Errorf("line %d: expected occurrence %d, got %d", i, w, res.Lines[i].Occurrence)
		}
	}
}

// TestParseFormatV2Snippet parses the real headerless export snippet (PRD 7.2).
func TestParseFormatV2Snippet(t *testing.T) {
	res, err := Parse(strings.NewReader(fixture(t, "format_v2_snippet.csv")))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(res.Lines) != 12 || len(res.Errors) != 0 || len(res.Warnings) != 0 {
		t.Fatalf("expected 12 clean lines, got %d lines, errors %v, warnings %v", len(res.Lines), res.Errors, res.Warnings)
	}

	first := res.Lines[0]
	if first.Message != "EXAMPLE STORE, CITY N " {
		t.Errorf("expected quoted message with trailing space, got %q", first.Message)
	}
	if first.Date != "2026-11-05" || first.AmountMinor != -48514 || first.BankRef != "1000000001" {
		t.Errorf("unexpected first line: %+v", first)
	}
	if first.BalanceMinor != nil {
		t.Errorf("expected nil balance, got %d", *first.BalanceMinor)
	}

	// MobilePay row: secondary reference (col 11) and extra text (col 14)
	mp := res.Lines[3]
	if mp.Message != "MobilePay Example Shop" || mp.ExtraRef != "11000000001" || mp.ExtraText != "MobilePay Example Shop" {
		t.Errorf("unexpected MobilePay line: %+v", mp)
	}

	// Card purchase booked the day after the transaction date (cols 7 vs 8)
	cardPurchase := res.Lines[8]
	if cardPurchase.TransactionDate == nil || *cardPurchase.TransactionDate != "2026-11-01" {
		t.Errorf("expected transaction date 2026-11-01, got %v", cardPurchase.TransactionDate)
	}
	if cardPurchase.Date != "2026-11-02" {
		t.Errorf("expected booking date 2026-11-02, got %q", cardPurchase.Date)
	}

	// Internal transfer: both account numbers (cols 2/3), positive amount
	tr := res.Lines[10]
	if tr.Message != "Person B" || tr.AccountFrom != "9999 0000000002" || tr.AccountTo != "9999 0000000001" || tr.AmountMinor != 450000 {
		t.Errorf("unexpected transfer line: %+v", tr)
	}

	// File is newest-first on the booking date; seq is the reverse
	if res.Lines[0].Seq != 12 || res.Lines[11].Seq != 1 {
		t.Errorf("expected seq 12/1, got %d/%d", res.Lines[0].Seq, res.Lines[11].Seq)
	}
}

func TestParseEmptyFile(t *testing.T) {
	_, err := Parse(strings.NewReader(fixture(t, "empty.csv")))
	if err == nil {
		t.Fatalf("expected error for empty file")
	}
}

func TestParseMinor(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"0", 0},
		{"1", 100},
		{"-1", -100},
		{"1.357", 135700},
		{"-1.357,97", -135797},
		{"25.000,00", 2500000},
		{"250,5", 25050},
		{"1.234.567,89", 123456789},
		{"-0,01", -1},
		{"1.000.000", 100000000},
	}
	for _, c := range cases {
		got, err := parseMinor(c.in)
		if err != nil {
			t.Errorf("parseMinor(%q) failed: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseMinor(%q) = %d, want %d", c.in, got, c.want)
		}
	}

	bad := []string{"", "-", "abc", "1,2,3", "1.2.3,4x", "-10,005", "1,234"}
	for _, in := range bad {
		if _, err := parseMinor(in); err == nil {
			t.Errorf("parseMinor(%q) expected error", in)
		}
	}
}

func TestParseDate(t *testing.T) {
	if got, err := parseDate("31-12-2024"); err != nil || got != "2024-12-31" {
		t.Errorf("parseDate(31-12-2024) = %q, %v", got, err)
	}
	if _, err := parseDate("2024-12-31"); err == nil {
		t.Errorf("parseDate(2024-12-31) expected error (wrong format)")
	}
	if _, err := parseDate("31-02-2025"); err == nil {
		t.Errorf("parseDate(31-02-2025) expected error (invalid calendar date)")
	}
	if d, err := parseOptionalDate(""); err != nil || d != nil {
		t.Errorf("parseOptionalDate(\"\") = %v, %v; want nil, nil", d, err)
	}
	if d, err := parseOptionalDate("05-11-2026"); err != nil || d == nil || *d != "2026-11-05" {
		t.Errorf("parseOptionalDate(05-11-2026) = %v, %v", d, err)
	}
}
