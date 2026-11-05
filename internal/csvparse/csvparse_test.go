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

	// File is newest-first; seq is the reverse (chronological, IMP-8a)
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
	if first.BalanceMinor != 7364203 {
		t.Errorf("expected balance 7364203, got %d", first.BalanceMinor)
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
	// Unquoted ';' in the message: middle fields are joined back with ';'
	if res.Lines[2].Message != "Køb; a; b" {
		t.Errorf("expected message %q, got %q", "Køb; a; b", res.Lines[2].Message)
	}
	if res.Lines[2].AmountMinor != -1000 {
		t.Errorf("expected -1000, got %d", res.Lines[2].AmountMinor)
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
	// Rows 1 and 3 are identical tuples -> occurrence 1 and 2; row 2 differs by balance -> occurrence 1
	want := []int{1, 1, 2, 1}
	for i, w := range want {
		if res.Lines[i].Occurrence != w {
			t.Errorf("line %d: expected occurrence %d, got %d", i, w, res.Lines[i].Occurrence)
		}
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
}
