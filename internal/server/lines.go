package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/mrFred489/financeanalysis/internal/db/dbq"
)

// lineFilters holds the parsed query parameters of GET /api/lines (STMT-2).
type lineFilters struct {
	from          string
	to            string
	accounts      string // JSON array, empty = all
	direction     string // "", "in", "out"
	amountMin     *int64
	amountMax     *int64
	message       string
	categories    string // JSON array, empty = all
	subcategories string // JSON array, empty = all
	source        string
	importID      *int64
	spreadGt      *int64
}

// lineJSON is the API representation of one statement line (STMT-1).
type lineJSON struct {
	ID                  int64   `json:"id"`
	AccountID           int64   `json:"accountId"`
	AccountName         string  `json:"accountName"`
	ImportID            *int64  `json:"importId"`
	BookingDate         string  `json:"bookingDate"`
	TransactionDate     *string `json:"transactionDate"`
	ValueDate           *string `json:"valueDate"`
	Message             string  `json:"message"`
	AmountMinor         int64   `json:"amountMinor"`
	BalanceMinor        *int64  `json:"balanceMinor"` // null when the bank format has no balance
	Currency            string  `json:"currency"`
	BankRef             *string `json:"bankRef"`
	CounterpartyAccount *string `json:"counterpartyAccount"`
	FileRow             int64   `json:"fileRow"`
	Seq                 int64   `json:"seq"`
	Occurrence          int64   `json:"occurrence"`
	SubcategoryID       *int64  `json:"subcategoryId"`
	SubcategoryName     *string `json:"subcategoryName"`
	SubcategoryColor    *string `json:"subcategoryColor"`
	CategoryID          *int64  `json:"categoryId"`
	CategoryName        *string `json:"categoryName"`
	Source              string  `json:"source"`
	RuleID              *int64  `json:"ruleId"`
	SpreadMonths        int64   `json:"spreadMonths"`
}

// linesResponse is the paginated result of GET /api/lines (STMT-3/STMT-4).
type linesResponse struct {
	Lines    []lineJSON `json:"lines"`
	Total    int64      `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"pageSize"`
	Totals   totalsJSON `json:"totals"`
}

type totalsJSON struct {
	Count  int64 `json:"count"`
	SumIn  int64 `json:"sumIn"`
	SumOut int64 `json:"sumOut"`
	Net    int64 `json:"net"`
}

const linesSelect = `SELECT
    sl.id, sl.account_id, a.name AS account_name, sl.import_id,
    sl.booking_date, sl.transaction_date, sl.value_date,
    sl.message, sl.amount_minor, sl.balance_minor, sl.currency,
    sl.bank_ref, sl.counterparty_account,
    sl.file_row, sl.seq, sl.occurrence,
    sl.subcategory_id, sc.name AS subcategory_name, sc.color AS subcategory_color,
    c.id AS category_id, c.name AS category_name,
    sl.source, sl.rule_id, sl.spread_months
FROM statement_line sl
JOIN account a ON a.id = sl.account_id
LEFT JOIN subcategory sc ON sc.id = sl.subcategory_id
LEFT JOIN category c ON c.id = sc.category_id`

// linesWhere is the shared filter predicate; each filter binds its value twice.
const linesWhere = `WHERE (? IS NULL OR sl.booking_date >= ?)
  AND (? IS NULL OR sl.booking_date <= ?)
  AND (? IS NULL OR sl.account_id IN (SELECT CAST(value AS INTEGER) FROM json_each(?)))
  AND (? IS NULL OR (? = 'in' AND sl.amount_minor > 0) OR (? = 'out' AND sl.amount_minor < 0))
  AND (? IS NULL OR sl.amount_minor >= ?)
  AND (? IS NULL OR sl.amount_minor <= ?)
  AND (? IS NULL OR sl.message LIKE '%' || ? || '%')
  AND (? IS NULL OR sc.category_id IN (SELECT CAST(value AS INTEGER) FROM json_each(?)))
  AND (? IS NULL OR sl.subcategory_id IN (SELECT CAST(value AS INTEGER) FROM json_each(?)))
  AND (? IS NULL OR sl.source IN (SELECT value FROM json_each(?)))
  AND (? IS NULL OR sl.import_id = ?)
  AND (? IS NULL OR sl.spread_months > ?)`

// filterArgs returns the positional args matching linesWhere.
func (f *lineFilters) filterArgs() []any {
	str := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	intp := func(p *int64) any {
		if p == nil {
			return nil
		}
		return *p
	}
	return []any{
		str(f.from), str(f.from),
		str(f.to), str(f.to),
		str(f.accounts), str(f.accounts),
		str(f.direction), str(f.direction), str(f.direction),
		intp(f.amountMin), intp(f.amountMin),
		intp(f.amountMax), intp(f.amountMax),
		str(f.message), str(f.message),
		str(f.categories), str(f.categories),
		str(f.subcategories), str(f.subcategories),
		str(f.source), str(f.source),
		intp(f.importID), intp(f.importID),
		intp(f.spreadGt), intp(f.spreadGt),
	}
}

// sqlcParams returns the equivalent params for the sqlc-generated count/totals queries.
func (f *lineFilters) sqlcParams() (from, to, accounts, direction, message, categories, subcategories, source any, amountMin, amountMax, importID, spreadGt any) {
	str := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	intp := func(p *int64) any {
		if p == nil {
			return nil
		}
		return *p
	}
	return str(f.from), str(f.to), str(f.accounts), str(f.direction),
		str(f.message), str(f.categories), str(f.subcategories), str(f.source),
		intp(f.amountMin), intp(f.amountMax), intp(f.importID), intp(f.spreadGt)
}

// parseIDList parses a comma-separated list of integer ids into a JSON array string.
func parseIDList(s string) string {
	if s == "" {
		return ""
	}
	var ids []int64
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return ""
	}
	b, _ := json.Marshal(ids)
	return string(b)
}

// validSources are the allowed values of statement_line.source (schema CHECK).
var validSources = map[string]bool{
	"none": true, "rule": true, "manual": true, "transfer": true, "conflict": true,
}

// parseSourceList parses the comma-separated `source` param (D6: single values
// keep working) into a JSON array string for the json_each() filter. Returns an
// error on unknown values.
func parseSourceList(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	var sources []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !validSources[part] {
			return "", fmt.Errorf("invalid source %q", part)
		}
		sources = append(sources, part)
	}
	if len(sources) == 0 {
		return "", nil
	}
	b, _ := json.Marshal(sources)
	return string(b), nil
}

var sortColumns = map[string]string{
	"date":    "sl.booking_date",
	"amount":  "sl.amount_minor",
	"message": "sl.message",
	"balance": "sl.balance_minor",
	"account": "a.name",
}

// parseLineFilters reads the query parameters of GET /api/lines.
func parseLineFilters(r *http.Request) (*lineFilters, string, string, int, int, error) {
	q := r.URL.Query()
	f := &lineFilters{
		from:          q.Get("from"),
		to:            q.Get("to"),
		accounts:      parseIDList(q.Get("accounts")),
		direction:     q.Get("direction"),
		message:       q.Get("message"),
		categories:    parseIDList(q.Get("categories")),
		subcategories: parseIDList(q.Get("subcategories")),
	}
	source, err := parseSourceList(q.Get("source"))
	if err != nil {
		return nil, "", "", 0, 0, err
	}
	f.source = source
	if f.direction != "" && f.direction != "in" && f.direction != "out" && f.direction != "both" {
		return nil, "", "", 0, 0, fmt.Errorf("invalid direction %q", f.direction)
	}
	if f.direction == "both" {
		f.direction = ""
	}
	if v := q.Get("amountMin"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, "", "", 0, 0, fmt.Errorf("invalid amountMin")
		}
		f.amountMin = &n
	}
	if v := q.Get("amountMax"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, "", "", 0, 0, fmt.Errorf("invalid amountMax")
		}
		f.amountMax = &n
	}
	if v := q.Get("importId"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, "", "", 0, 0, fmt.Errorf("invalid importId")
		}
		f.importID = &n
	}
	if v := q.Get("spreadGt"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, "", "", 0, 0, fmt.Errorf("invalid spreadGt")
		}
		f.spreadGt = &n
	}

	sort := q.Get("sort")
	if sort == "" {
		sort = "date"
	}
	if _, ok := sortColumns[sort]; !ok {
		return nil, "", "", 0, 0, fmt.Errorf("invalid sort %q", sort)
	}
	order := q.Get("order")
	if order == "" {
		order = "desc"
	}
	if order != "asc" && order != "desc" {
		return nil, "", "", 0, 0, fmt.Errorf("invalid order %q", order)
	}

	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(q.Get("pageSize"))
	if pageSize < 1 {
		pageSize = 50
	}
	if pageSize > 500 {
		pageSize = 500
	}
	return f, sort, order, page, pageSize, nil
}

func (s *Server) handleListLines() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		f, sort, order, page, pageSize, err := parseLineFilters(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_params", Message: err.Error()})
			return
		}
		ctx := r.Context()
		q := dbq.New(d)

		// Count + totals for the filtered set (STMT-4)
		from, to, accounts, direction, message, categories, subcategories, source, amountMin, amountMax, importID, spreadGt := f.sqlcParams()
		countParams := dbq.CountStatementLinesParams{
			FromDate: from, ToDate: to, Accounts: accounts, Direction: direction,
			AmountMin: amountMin, AmountMax: amountMax, Message: message,
			Categories: categories, Subcategories: subcategories, Source: source,
			ImportID: importID, SpreadGt: spreadGt,
		}
		total, err := q.CountStatementLines(ctx, countParams)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		totalsRow, err := q.StatementLineTotals(ctx, dbq.StatementLineTotalsParams{
			FromDate: from, ToDate: to, Accounts: accounts, Direction: direction,
			AmountMin: amountMin, AmountMax: amountMax, Message: message,
			Categories: categories, Subcategories: subcategories, Source: source,
			ImportID: importID, SpreadGt: spreadGt,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}

		// List with server-side sorting + pagination (STMT-3). sqlc cannot
		// parameterize ORDER BY, so the whitelisted sort column is inlined here.
		orderCol := sortColumns[sort]
		orderDir := "DESC"
		if order == "asc" {
			orderDir = "ASC"
		}
		var orderBy string
		if sort == "date" {
			orderBy = fmt.Sprintf("ORDER BY sl.booking_date %s, sl.seq %s, sl.id", orderDir, orderDir)
		} else {
			orderBy = fmt.Sprintf("ORDER BY %s %s, sl.id", orderCol, orderDir)
		}
		limit := int64(pageSize)
		offset := int64((page - 1) * pageSize)
		query := linesSelect + " " + linesWhere + " " + orderBy + " LIMIT ? OFFSET ?"
		args := append(f.filterArgs(), limit, offset)

		rows, err := d.QueryContext(ctx, query, args...)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		defer rows.Close()
		lines := []lineJSON{}
		for rows.Next() {
			var row dbq.ListStatementLinesRow
			if err := rows.Scan(
				&row.ID, &row.AccountID, &row.AccountName, &row.ImportID,
				&row.BookingDate, &row.TransactionDate, &row.ValueDate,
				&row.Message, &row.AmountMinor, &row.BalanceMinor, &row.Currency,
				&row.BankRef, &row.CounterpartyAccount,
				&row.FileRow, &row.Seq, &row.Occurrence,
				&row.SubcategoryID, &row.SubcategoryName, &row.SubcategoryColor,
				&row.CategoryID, &row.CategoryName,
				&row.Source, &row.RuleID, &row.SpreadMonths,
			); err != nil {
				writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
				return
			}
			lines = append(lines, lineJSON{
				ID: row.ID, AccountID: row.AccountID, AccountName: row.AccountName,
				ImportID:            nullIntPtr(row.ImportID),
				BookingDate:         row.BookingDate,
				TransactionDate:     nullStrPtr(row.TransactionDate),
				ValueDate:           nullStrPtr(row.ValueDate),
				Message:             row.Message,
				AmountMinor:         row.AmountMinor,
				BalanceMinor:        nullIntPtr(row.BalanceMinor),
				Currency:            row.Currency,
				BankRef:             nullStrPtr(row.BankRef),
				CounterpartyAccount: nullStrPtr(row.CounterpartyAccount),
				FileRow:             row.FileRow, Seq: row.Seq, Occurrence: row.Occurrence,
				SubcategoryID:    nullIntPtr(row.SubcategoryID),
				SubcategoryName:  nullStrPtr(row.SubcategoryName),
				SubcategoryColor: nullStrPtr(row.SubcategoryColor),
				CategoryID:       nullIntPtr(row.CategoryID),
				CategoryName:     nullStrPtr(row.CategoryName),
				Source:           row.Source, RuleID: nullIntPtr(row.RuleID), SpreadMonths: row.SpreadMonths,
			})
		}
		if err := rows.Err(); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, linesResponse{
			Lines:    lines,
			Total:    total,
			Page:     page,
			PageSize: pageSize,
			Totals: totalsJSON{
				Count:  totalsRow.LineCount,
				SumIn:  asInt64(totalsRow.SumIn),
				SumOut: asInt64(totalsRow.SumOut),
				Net:    asInt64(totalsRow.Net),
			},
		})
	}
}

func (s *Server) handleGetLine() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_id", Message: "invalid line id"})
			return
		}
		row, err := dbq.New(d).GetStatementLine(r.Context(), id)
		if err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusNotFound, APIError{Code: "not_found", Message: "line not found"})
				return
			}
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, lineJSON{
			ID: row.ID, AccountID: row.AccountID, AccountName: row.AccountName,
			ImportID:            nullIntPtr(row.ImportID),
			BookingDate:         row.BookingDate,
			TransactionDate:     nullStrPtr(row.TransactionDate),
			ValueDate:           nullStrPtr(row.ValueDate),
			Message:             row.Message,
			AmountMinor:         row.AmountMinor,
			BalanceMinor:        nullIntPtr(row.BalanceMinor),
			Currency:            row.Currency,
			BankRef:             nullStrPtr(row.BankRef),
			CounterpartyAccount: nullStrPtr(row.CounterpartyAccount),
			FileRow:             row.FileRow, Seq: row.Seq, Occurrence: row.Occurrence,
			SubcategoryID:    nullIntPtr(row.SubcategoryID),
			SubcategoryName:  nullStrPtr(row.SubcategoryName),
			SubcategoryColor: nullStrPtr(row.SubcategoryColor),
			CategoryID:       nullIntPtr(row.CategoryID),
			CategoryName:     nullStrPtr(row.CategoryName),
			Source:           row.Source, RuleID: nullIntPtr(row.RuleID), SpreadMonths: row.SpreadMonths,
		})
	}
}

func nullIntPtr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

func nullStrPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}

// asInt64 converts a SUM() result (typed interface{} by sqlc) to int64.
func asInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	case nil:
		return 0
	default:
		return 0
	}
}
