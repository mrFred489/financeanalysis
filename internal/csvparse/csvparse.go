// Package csvparse parses Danish bank CSV statement exports.
//
// Format (IMP-2): `;`-separated, newest row first. The first field is the
// booking date (dd-mm-yyyy), the last three fields are amount, balance and
// currency, and everything in between is joined back with ';' as the message,
// so ';' inside messages is tolerated. Standard CSV quoting is handled.
// Amounts use the Danish number format: '.' thousands separator, ',' decimal
// separator, stored as integer minor units (øre).
package csvparse

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// RawLine is one parsed CSV row, before duplicate detection.
type RawLine struct {
	Date         string `json:"date"`          // YYYY-MM-DD
	Message      string `json:"message"`       // middle fields joined with ';'
	AmountMinor  int64  `json:"amount_minor"`  // øre, negative for expenses
	BalanceMinor int64  `json:"balance_minor"` // øre
	Currency     string `json:"currency"`
	FileRow      int    `json:"file_row"`   // 1-based row number in the file
	Seq          int    `json:"seq"`        // chronological sequence (reverse of file order, IMP-8a)
	Occurrence   int    `json:"occurrence"` // 1-based index among identical tuples within the file
}

// ParseError describes a row that could not be parsed (IMP-3) or was excluded with a warning (IMP-4).
type ParseError struct {
	Row    int    `json:"row"`
	Reason string `json:"reason"`
}

// Result is the outcome of parsing a file.
type Result struct {
	Lines    []RawLine    `json:"lines"`    // in file order (newest first)
	Errors   []ParseError `json:"errors"`   // rows that failed parsing
	Warnings []ParseError `json:"warnings"` // rows excluded with a warning (e.g. non-DKK)
}

// Parse reads a bank CSV export and returns the parsed lines.
// It returns an error only when the file contains no data rows at all.
func Parse(r io.Reader) (*Result, error) {
	cr := csv.NewReader(r)
	cr.Comma = ';'
	cr.FieldsPerRecord = -1 // message may contain unquoted ';', producing extra fields
	cr.LazyQuotes = true
	cr.TrimLeadingSpace = false

	res := &Result{}
	rowNum := 0
	seen := map[string]int{}

	for {
		record, err := cr.Read()
		if err == io.EOF {
			break
		}
		rowNum++
		if err != nil {
			res.Errors = append(res.Errors, ParseError{Row: rowNum, Reason: fmt.Sprintf("invalid CSV: %v", err)})
			continue
		}
		// Strip a UTF-8 BOM from the first field of the first row
		if rowNum == 1 && len(record) > 0 {
			record[0] = strings.TrimPrefix(record[0], "\ufeff")
		}
		if isEmptyRow(record) {
			continue
		}
		if len(record) < 5 {
			res.Errors = append(res.Errors, ParseError{
				Row:    rowNum,
				Reason: fmt.Sprintf("expected at least 5 fields (date;...;amount;balance;currency), got %d", len(record)),
			})
			continue
		}

		date, err := parseDate(strings.TrimSpace(record[0]))
		if err != nil {
			res.Errors = append(res.Errors, ParseError{Row: rowNum, Reason: fmt.Sprintf("invalid date %q: %v", record[0], err)})
			continue
		}
		amount, err := parseMinor(strings.TrimSpace(record[len(record)-3]))
		if err != nil {
			res.Errors = append(res.Errors, ParseError{Row: rowNum, Reason: fmt.Sprintf("invalid amount %q: %v", record[len(record)-3], err)})
			continue
		}
		balance, err := parseMinor(strings.TrimSpace(record[len(record)-2]))
		if err != nil {
			res.Errors = append(res.Errors, ParseError{Row: rowNum, Reason: fmt.Sprintf("invalid balance %q: %v", record[len(record)-2], err)})
			continue
		}
		currency := strings.ToUpper(strings.TrimSpace(record[len(record)-1]))
		message := strings.Join(record[1:len(record)-3], ";")

		if currency != "DKK" {
			// IMP-4: non-DKK rows are reported and excluded.
			res.Warnings = append(res.Warnings, ParseError{
				Row:    rowNum,
				Reason: fmt.Sprintf("currency %q is not supported, row excluded", currency),
			})
			continue
		}

		key := date + "\x00" + strconv.FormatInt(amount, 10) + "\x00" + strconv.FormatInt(balance, 10) + "\x00" + message
		seen[key]++

		res.Lines = append(res.Lines, RawLine{
			Date:         date,
			Message:      message,
			AmountMinor:  amount,
			BalanceMinor: balance,
			Currency:     currency,
			FileRow:      rowNum,
			Occurrence:   seen[key],
		})
	}

	if len(res.Lines) == 0 && len(res.Errors) == 0 {
		return nil, fmt.Errorf("file contains no data rows")
	}

	// IMP-8a: the file is newest-first, so chronological seq is the reverse of file order.
	n := len(res.Lines)
	for i := range res.Lines {
		res.Lines[i].Seq = n - i
	}

	return res, nil
}

func isEmptyRow(record []string) bool {
	for _, f := range record {
		if strings.TrimSpace(f) != "" {
			return false
		}
	}
	return true
}

// parseDate converts dd-mm-yyyy to YYYY-MM-DD, validating the calendar date.
func parseDate(s string) (string, error) {
	t, err := time.Parse("02-01-2006", s)
	if err != nil {
		return "", err
	}
	return t.Format("2006-01-02"), nil
}

// parseMinor parses a Danish-formatted number into integer minor units (øre).
// '.' is the thousands separator, ',' the decimal separator.
// Examples: "-1.357,97" -> -135797, "1.357" -> 135700, "250,5" -> 25050.
func parseMinor(s string) (int64, error) {
	orig := s
	neg := false
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	} else if strings.HasPrefix(s, "+") {
		s = s[1:]
	}
	if s == "" {
		return 0, fmt.Errorf("empty number")
	}

	intPart := s
	fracPart := ""
	if i := strings.LastIndex(s, ","); i >= 0 {
		intPart, fracPart = s[:i], s[i+1:]
	}
	intPart = strings.ReplaceAll(intPart, ".", "")
	if intPart == "" {
		intPart = "0"
	}
	if !isDigits(intPart) || (fracPart != "" && !isDigits(fracPart)) {
		return 0, fmt.Errorf("not a number: %q", orig)
	}
	if len(fracPart) > 2 {
		return 0, fmt.Errorf("more than 2 decimal places: %q", orig)
	}
	fracPart += strings.Repeat("0", 2-len(fracPart))

	v, err := strconv.ParseInt(intPart+fracPart, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("number out of range: %q", orig)
	}
	if neg {
		v = -v
	}
	return v, nil
}

func isDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}
