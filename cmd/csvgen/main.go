// Command csvgen generates high-variance bank CSV files (format v2, PRD
// section 7.2) for testing the import functionality.
//
// Every file sticks to the known 16-column `;`-separated layout with no
// header row, sorted newest-first on the booking date (column 8) as the
// parser expects. Columns 5 (balance) and 6 (currency) are always empty,
// matching the current bank export. Messages frequently contain special
// characters (;, ", ,, æøå, emoji, ...) and amounts span 1 øre to
// 1.000.000.000 øre with random sign. A small share of rows are transfers
// between --account and --transfer-account so generated files interlock
// with the existing test fixtures, and a small share are exact duplicates
// (unique bank reference) to exercise duplicate detection.
//
// Each generated file is verified by parsing it back with
// internal/csvparse; the command exits non-zero if any row fails to parse.
//
// Usage:
//
//	go run ./cmd/csvgen --lines 5000
//	task gen:csv -- --lines 5000 --files 3
package main

import (
	"bytes"
	"encoding/csv"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mrFred489/financeanalysis/internal/csvparse"
)

const (
	// amountMaxMinor is the largest amount magnitude generated, in øre
	// (1.000.000.000 øre = 10.000.000,00 kr.); the smallest is 1 øre.
	amountMaxMinor = 1_000_000_000
	// specialRatio is the share of rows whose message gets a special
	// character token appended.
	specialRatio = 0.30
	// duplicateRatio is the share of rows that are exact duplicates of the
	// previous row (same date, amount and message; unique bank reference).
	duplicateRatio = 0.02
	// dateOffsetDays is the max random offset between transaction date,
	// booking date and value date.
	dateOffsetDays = 3
)

var messagePool = []string{
	"Løn december",
	"Køb i Netto",
	"Kontanthævning",
	"MobilePay Example Shop",
	"MobilePay Person A",
	"Overførsel",
	"Husleje",
	"Elregning",
	"EXAMPLE STORE, CITY N",
	"example-shop.com Nota aa-0000001",
	"Me & Example Nota A000001",
	`He said "hi"`,
	"Betaling; kort",
	"Æbleskiver på fad ÆØÅ",
	"Abonnement Streaming",
	"Dankort",
	"",
}

var specialPool = []string{
	";",
	`"`,
	",",
	"æøå ÆØÅ",
	"😀 🧾",
	"\t",
	" & < >",
	"trailing ",
	" leading",
	"semi;colon;inside",
	` "quoted" `,
	`mixed ";, combo`,
	"x" + strings.Repeat("y", 499),
}

type config struct {
	out             string
	files           int
	lines           int
	seed            uint64
	account         string
	transferAccount string
	transferRatio   float64
	start           time.Time
	end             time.Time
}

type row struct {
	booking     time.Time
	transaction time.Time
	value       time.Time
	message     string
	accountFrom string
	accountTo   string
	amount      int64 // øre
	bankRef     string
	extraRef    string
	extraText   string
}

type stats struct {
	transfers  int
	duplicates int
}

func main() {
	var (
		out             = flag.String("out", "testdata/generated", "output directory for generated CSV files")
		files           = flag.Int("files", 1, "number of files to generate (all share the same account pair)")
		lines           = flag.Int("lines", 1000, "data rows per file")
		seed            = flag.Uint64("seed", 42, "random seed (reproducible output for a given seed and date range)")
		account         = flag.String("account", "9999 0000000001", "from-account on ordinary rows; matches the existing test account")
		transferAccount = flag.String("transfer-account", "9999 0000000002", "the other side of transfer rows; matches the existing test fixtures")
		transferRatio   = flag.Float64("transfer-ratio", 0.005, "share of rows that are transfers between the two accounts (0.005 = 0.5%%)")
		startStr        = flag.String("start", "", "oldest booking date, YYYY-MM-DD (default: one year before --end)")
		endStr          = flag.String("end", "", "newest booking date, YYYY-MM-DD (default: today)")
	)
	flag.Parse()

	cfg := config{
		out:             *out,
		files:           *files,
		lines:           *lines,
		seed:            *seed,
		account:         *account,
		transferAccount: *transferAccount,
		transferRatio:   *transferRatio,
	}
	if cfg.lines <= 0 {
		fatal("--lines must be positive")
	}
	if cfg.files <= 0 {
		fatal("--files must be positive")
	}
	if cfg.transferRatio < 0 || cfg.transferRatio > 1 {
		fatal("--transfer-ratio must be within [0, 1]")
	}

	// Default date range: the last year up to today (UTC).
	now := time.Now().UTC()
	cfg.end = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	cfg.start = cfg.end.AddDate(-1, 0, 0)
	var err error
	if *endStr != "" {
		if cfg.end, err = time.Parse("2006-01-02", *endStr); err != nil {
			fatal("invalid --end %q: %v", *endStr, err)
		}
	}
	if *startStr != "" {
		if cfg.start, err = time.Parse("2006-01-02", *startStr); err != nil {
			fatal("invalid --start %q: %v", *startStr, err)
		}
	}
	if cfg.start.After(cfg.end) {
		fatal("--start must not be after --end")
	}

	if err := os.MkdirAll(cfg.out, 0o755); err != nil {
		fatal("cannot create output directory: %v", err)
	}

	acctTag := strings.NewReplacer(" ", "-", "/", "-").Replace(cfg.account)
	for f := 0; f < cfg.files; f++ {
		rng := rand.New(rand.NewPCG(cfg.seed+uint64(f), cfg.seed^0x9e3779b97f4a7c15))
		rows, st := generate(rng, cfg, f)

		name := fmt.Sprintf("gen-%s-%03d.csv", acctTag, f)
		path := filepath.Join(cfg.out, name)
		if err := writeFile(path, rows); err != nil {
			fatal("writing %s: %v", path, err)
		}
		if err := verify(path, len(rows)); err != nil {
			fatal("generated file %s failed verification: %v", path, err)
		}

		fmt.Printf("%s: %d lines (%d transfers, %d duplicates), %s .. %s (newest first)\n",
			path, len(rows), st.transfers, st.duplicates,
			formatDate(rows[0].booking), formatDate(rows[len(rows)-1].booking))
	}
}

// generate builds the rows for one file in random order; the caller sorts
// them newest-first on the booking date.
func generate(rng *rand.Rand, cfg config, fileIdx int) ([]row, stats) {
	spanDays := int(cfg.end.Sub(cfg.start).Hours()/24) + 1
	rows := make([]row, 0, cfg.lines)
	var st stats

	for i := 0; i < cfg.lines; i++ {
		refNum := int64(fileIdx)*1_000_000_000 + int64(i) + 1

		// ~2% exact duplicates of the previous row (unique bank reference).
		if i > 0 && rng.Float64() < duplicateRatio {
			dup := rows[len(rows)-1]
			dup.bankRef = fmt.Sprintf("%010d", refNum)
			rows = append(rows, dup)
			st.duplicates++
			continue
		}

		booking := cfg.start.AddDate(0, 0, rng.IntN(spanDays))
		transaction := booking.AddDate(0, 0, -rng.IntN(dateOffsetDays+1))
		value := booking.AddDate(0, 0, rng.IntN(dateOffsetDays+1))

		from, to := cfg.account, ""
		var amount int64
		if rng.Float64() < cfg.transferRatio {
			// Transfer: both account columns set, positive amount. The side
			// that is not the imported account becomes the counterparty.
			if rng.Float64() < 0.5 {
				from, to = cfg.transferAccount, cfg.account
			} else {
				from, to = cfg.account, cfg.transferAccount
			}
			amount = 1 + rng.Int64N(amountMaxMinor)
			st.transfers++
		} else {
			amount = 1 + rng.Int64N(amountMaxMinor)
			if rng.Float64() < 0.5 {
				amount = -amount
			}
		}

		message := messagePool[rng.IntN(len(messagePool))]
		if rng.Float64() < specialRatio {
			message += " " + specialPool[rng.IntN(len(specialPool))]
		}

		r := row{
			booking:     booking,
			transaction: transaction,
			value:       value,
			message:     message,
			accountFrom: from,
			accountTo:   to,
			amount:      amount,
			bankRef:     fmt.Sprintf("%010d", refNum),
		}
		// MobilePay-style rows carry a secondary reference and repeat the
		// message in the extra-text column, like the real export.
		if strings.HasPrefix(message, "MobilePay") {
			r.extraRef = fmt.Sprintf("%011d", refNum+10_000_000_000)
			r.extraText = message
		}
		rows = append(rows, r)
	}

	// Newest first on the booking date, as the v2 format requires (IMP-8a).
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].booking.After(rows[j].booking) })
	return rows, st
}

// writeFile writes the rows in the fixed 16-column v2 layout. Columns 0, 5,
// 6, 12, 13, 14 (unless MobilePay) and 15 are empty.
func writeFile(path string, rows []row) error {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	w.Comma = ';'
	for _, r := range rows {
		record := []string{
			"", // 0: leading column, always empty
			r.message,
			r.accountFrom,
			r.accountTo,
			formatMinor(r.amount),
			"", // 5: balance (empty in the current export)
			"", // 6: currency (empty means DKK)
			formatDate(r.transaction),
			formatDate(r.booking),
			formatDate(r.value),
			r.bankRef,
			r.extraRef,
			"", // 12: reserved
			"", // 13: reserved
			r.extraText,
			"", // 15: reserved
		}
		if err := w.Write(record); err != nil {
			return err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// verify parses the generated file back with the real parser and checks that
// every row parsed and none produced errors or warnings.
func verify(path string, wantLines int) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	res, err := csvparse.Parse(bytes.NewReader(data))
	if err != nil {
		return err
	}
	if len(res.Errors) > 0 {
		e := res.Errors[0]
		return fmt.Errorf("%d parse errors, first at row %d: %s", len(res.Errors), e.Row, e.Reason)
	}
	if len(res.Warnings) > 0 {
		e := res.Warnings[0]
		return fmt.Errorf("%d warnings, first at row %d: %s", len(res.Warnings), e.Row, e.Reason)
	}
	if len(res.Lines) != wantLines {
		return fmt.Errorf("expected %d parsed lines, got %d", wantLines, len(res.Lines))
	}
	return nil
}

// formatMinor renders øre in the Danish number format: '.' thousands
// separator, ',' decimal separator, e.g. -135797 -> "-1.357,97".
func formatMinor(v int64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := groupThousands(strconv.FormatInt(v/100, 10)) + "," + fmt.Sprintf("%02d", v%100)
	if neg {
		s = "-" + s
	}
	return s
}

func groupThousands(s string) string {
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
	}
	for i := pre; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte('.')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// formatDate renders dd-mm-yyyy, the date format of the v2 export.
func formatDate(t time.Time) string {
	return t.Format("02-01-2006")
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "csvgen: "+format+"\n", args...)
	os.Exit(1)
}
