// Package csvparse parses Danish bank CSV statement exports (format v2, see
// PRD section 7.2).
//
// Format: UTF-8 (BOM tolerated and stripped), no header row, `;`-separated,
// standard CSV quoting, newest row first (sorted on the booking date). Fixed
// 16 columns, 0-indexed; every row starts with an empty column 0:
//
//	0  empty                8  booking date (dd-mm-yyyy)   <- canonical date
//	1  message              9  value date (dd-mm-yyyy)
//	2  account number      10  bank reference
//	3  counterparty acct.  11  secondary reference (e.g. MobilePay id)
//	4  amount (da-DK)      12-13 reserved
//	5  balance (optional)  14  extra text
//	6  currency (optional) 15  reserved
//	7  transaction date (dd-mm-yyyy)
//
// Columns 5 (balance) and 6 (currency) are empty in the current export: an
// empty balance is returned as nil, an empty currency defaults to DKK. Rows
// with more than 16 fields (an unquoted ';' in the message) are tolerated by
// joining the surplus fields back into the message; rows with fewer than 16
// fields are parse errors. Amounts use the Danish number format: '.'
// thousands separator, ',' decimal separator, stored as integer minor units
// (øre).
package csvparse

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// numFields is the fixed column count of the v2 format (columns 0..15).
const numFields = 16

// RawLine is one parsed CSV row, before duplicate detection.
type RawLine struct {
	Date            string  `json:"date"`                      // YYYY-MM-DD, column 8 (booking date)
	TransactionDate *string `json:"transactionDate,omitempty"` // column 7, nil when empty
	ValueDate       *string `json:"valueDate,omitempty"`       // column 9, nil when empty
	Message         string  `json:"message"`                   // column 1
	AccountFrom     string  `json:"accountFrom"`               // column 2
	AccountTo       string  `json:"accountTo,omitempty"`       // column 3, transfers only
	AmountMinor     int64   `json:"amountMinor"`               // column 4, øre, negative for expenses
	BalanceMinor    *int64  `json:"balanceMinor,omitempty"`    // column 5, nil when the export has no balance
	Currency        string  `json:"currency"`                  // column 6, "DKK" when empty
	BankRef         string  `json:"bankRef,omitempty"`         // column 10
	ExtraRef        string  `json:"extraRef,omitempty"`        // column 11
	ExtraText       string  `json:"extraText,omitempty"`       // column 14
	FileRow         int     `json:"fileRow"`                   // 1-based row number in the file
	Seq             int     `json:"seq"`                       // chronological sequence (reverse of file order, IMP-8a)
	Occurrence      int     `json:"occurrence"`                // 1-based index among identical tuples within the file
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
	cr.FieldsPerRecord = -1 // an unquoted ';' in the message produces extra fields
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
		if len(record) < numFields {
			res.Errors = append(res.Errors, ParseError{
				Row:    rowNum,
				Reason: fmt.Sprintf("expected at least %d fields, got %d", numFields, len(record)),
			})
			continue
		}

		// Fixed layout from column 2 on: the last 14 fields are columns 2..15;
		// everything between column 0 and them is the message (tolerates an
		// unquoted ';' inside the message).
		message := strings.Join(record[1:len(record)-14], ";")
		tail := record[len(record)-14:]
		accountFrom := strings.TrimSpace(tail[0])
		accountTo := strings.TrimSpace(tail[1])
		amountStr := strings.TrimSpace(tail[2])
		balanceStr := strings.TrimSpace(tail[3])
		currencyStr := strings.TrimSpace(tail[4])
		transactionStr := strings.TrimSpace(tail[5])
		bookingStr := strings.TrimSpace(tail[6])
		valueStr := strings.TrimSpace(tail[7])
		bankRef := strings.TrimSpace(tail[8])
		extraRef := strings.TrimSpace(tail[9])
		extraText := strings.TrimSpace(tail[12])

		date, err := parseDate(bookingStr)
		if err != nil {
			res.Errors = append(res.Errors, ParseError{Row: rowNum, Reason: fmt.Sprintf("invalid booking date %q: %v", bookingStr, err)})
			continue
		}
		transactionDate, err := parseOptionalDate(transactionStr)
		if err != nil {
			res.Errors = append(res.Errors, ParseError{Row: rowNum, Reason: fmt.Sprintf("invalid transaction date %q: %v", transactionStr, err)})
			continue
		}
		valueDate, err := parseOptionalDate(valueStr)
		if err != nil {
			res.Errors = append(res.Errors, ParseError{Row: rowNum, Reason: fmt.Sprintf("invalid value date %q: %v", valueStr, err)})
			continue
		}
		amount, err := parseMinor(amountStr)
		if err != nil {
			res.Errors = append(res.Errors, ParseError{Row: rowNum, Reason: fmt.Sprintf("invalid amount %q: %v", amountStr, err)})
			continue
		}
		var balance *int64
		if balanceStr != "" {
			b, err := parseMinor(balanceStr)
			if err != nil {
				res.Errors = append(res.Errors, ParseError{Row: rowNum, Reason: fmt.Sprintf("invalid balance %q: %v", balanceStr, err)})
				continue
			}
			balance = &b
		}
		currency := strings.ToUpper(currencyStr)
		if currency == "" {
			currency = "DKK"
		}

		if currency != "DKK" {
			// IMP-4: non-DKK rows are reported and excluded.
			res.Warnings = append(res.Warnings, ParseError{
				Row:    rowNum,
				Reason: fmt.Sprintf("currency %q is not supported, row excluded", currency),
			})
			continue
		}

		// The dedup/occurrence key has no balance: the current format has no
		// running balance (PRD section 6).
		key := date + "\x00" + strconv.FormatInt(amount, 10) + "\x00" + message
		seen[key]++

		res.Lines = append(res.Lines, RawLine{
			Date:            date,
			TransactionDate: transactionDate,
			ValueDate:       valueDate,
			Message:         message,
			AccountFrom:     accountFrom,
			AccountTo:       accountTo,
			AmountMinor:     amount,
			BalanceMinor:    balance,
			Currency:        currency,
			BankRef:         bankRef,
			ExtraRef:        extraRef,
			ExtraText:       extraText,
			FileRow:         rowNum,
			Occurrence:      seen[key],
		})
	}

	if len(res.Lines) == 0 && len(res.Errors) == 0 {
		return nil, fmt.Errorf("file contains no data rows")
	}

	// IMP-8a: the file is newest-first (on the booking date), so chronological
	// seq is the reverse of file order.
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

// parseOptionalDate parses dd-mm-yyyy; an empty string yields nil (no error).
func parseOptionalDate(s string) (*string, error) {
	if s == "" {
		return nil, nil
	}
	d, err := parseDate(s)
	if err != nil {
		return nil, err
	}
	return &d, nil
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
