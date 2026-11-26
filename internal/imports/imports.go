// Package imports implements the two-phase CSV import flow (IMP-5..IMP-13):
// Preview (parse + checks, nothing written), Commit (backup + transactional
// insert of non-duplicate lines) and UndoLatest (delete the latest non-undone
// import batch's lines).
package imports

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/mrFred489/financeanalysis/internal/actions"
	"github.com/mrFred489/financeanalysis/internal/csvparse"
	"github.com/mrFred489/financeanalysis/internal/db"
	"github.com/mrFred489/financeanalysis/internal/db/dbq"
	"github.com/mrFred489/financeanalysis/internal/rules"
	"github.com/mrFred489/financeanalysis/internal/transfers"
)

// ErrInvalidPreviewToken is returned when the preview token does not match the
// uploaded file and account (tamper protection between the two phases).
var ErrInvalidPreviewToken = errors.New("preview token does not match the uploaded file")

// ErrNoImportToUndo is returned when there is no non-undone import batch.
var ErrNoImportToUndo = errors.New("no import to undo")

// Warning is a non-blocking finding shown in the preview screen (IMP-5).
type Warning struct {
	Type    string `json:"type"` // account_mismatch | duplicate_file | non_dkk
	Message string `json:"message"`
	Row     int    `json:"row,omitempty"`
}

// PreviewResult is the JSON returned by the preview phase.
type PreviewResult struct {
	FileName       string                `json:"fileName"`
	FileSHA256     string                `json:"fileSha256"`
	PreviewToken   string                `json:"previewToken"`
	AccountID      int64                 `json:"accountId"`
	FileAccounts   []string              `json:"fileAccounts"` // account numbers found in columns 2/3 of the file
	DateFrom       string                `json:"dateFrom,omitempty"`
	DateTo         string                `json:"dateTo,omitempty"`
	NewCount       int                   `json:"newCount"`
	DuplicateCount int                   `json:"duplicateCount"`
	ParseErrors    []csvparse.ParseError `json:"parseErrors"`
	Warnings       []Warning             `json:"warnings"`
}

// CommitResult is the JSON returned by the commit phase (IMP-12).
type CommitResult struct {
	ImportID          int64                 `json:"importId"`
	FileName          string                `json:"fileName"`
	LinesImported     int                   `json:"linesImported"`
	DuplicatesSkipped int                   `json:"duplicatesSkipped"`
	Uncategorized     int                   `json:"uncategorized"`     // source = 'none' after rule evaluation
	RuleCategorized   int                   `json:"ruleCategorized"`   // IMP-11/12: source = 'rule'
	RuleConflicts     int                   `json:"ruleConflicts"`     // IMP-12: source = 'conflict'
	TransferMatched   int                   `json:"transferMatched"`   // M4 IMP-11/12: pairs auto-linked as transfers
	TransferAmbiguous int                   `json:"transferAmbiguous"` // M4 IMP-12: ambiguous candidate groups
	ParseErrors       []csvparse.ParseError `json:"parseErrors"`
}

// NewSecret generates a random secret used to sign preview tokens.
func NewSecret() ([]byte, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	return secret, nil
}

// SignPreview creates the token binding a preview to (account, file name, file hash).
func SignPreview(secret []byte, accountID int64, fileName, fileSHA string) string {
	mac := hmac.New(sha256.New, secret)
	fmt.Fprintf(mac, "%d|%s|%s", accountID, fileName, fileSHA)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyPreview checks a token against the given values.
func VerifyPreview(secret []byte, token string, accountID int64, fileName, fileSHA string) bool {
	want := SignPreview(secret, accountID, fileName, fileSHA)
	return hmac.Equal([]byte(want), []byte(token))
}

// SHA256Hex returns the hex SHA-256 of content.
func SHA256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// Preview parses the uploaded file and computes all checks without writing anything (IMP-5).
func Preview(ctx context.Context, q *dbq.Queries, secret []byte, accountID int64, fileName string, content []byte) (*PreviewResult, error) {
	parsed, err := csvparse.Parse(bytes.NewReader(content))
	if err != nil {
		return nil, err
	}

	sha := SHA256Hex(content)
	res := &PreviewResult{
		FileName:     fileName,
		FileSHA256:   sha,
		AccountID:    accountID,
		ParseErrors:  parsed.Errors,
		PreviewToken: SignPreview(secret, accountID, fileName, sha),
	}

	// IMP-9: identical file already imported?
	if n, err := q.ImportBatchExistsBySHA(ctx, sha); err != nil {
		return nil, err
	} else if n > 0 {
		res.Warnings = append(res.Warnings, Warning{
			Type:    "duplicate_file",
			Message: "This exact file was already imported (same SHA-256). All its lines will be skipped as duplicates.",
		})
	}

	// IMP-4 warnings from the parser
	for _, w := range parsed.Warnings {
		res.Warnings = append(res.Warnings, Warning{Type: "non_dkk", Message: w.Reason, Row: w.Row})
	}

	// Account numbers found in columns 2/3 of the file (IMP-8 sanity check + preview info)
	res.FileAccounts = fileAccounts(parsed.Lines)

	// IMP-6: duplicate detection against existing lines
	newLines := make([]csvparse.RawLine, 0, len(parsed.Lines))
	for _, line := range parsed.Lines {
		n, err := q.LineExists(ctx, dbq.LineExistsParams{
			AccountID:   accountID,
			BookingDate: line.Date,
			AmountMinor: line.AmountMinor,
			Message:     line.Message,
			Occurrence:  int64(line.Occurrence),
		})
		if err != nil {
			return nil, err
		}
		if n > 0 {
			res.DuplicateCount++
		} else {
			newLines = append(newLines, line)
		}
	}
	res.NewCount = len(newLines)

	if len(parsed.Lines) > 0 {
		res.DateFrom = parsed.Lines[len(parsed.Lines)-1].Date // oldest = last in file
		res.DateTo = parsed.Lines[0].Date                     // newest = first in file
	}

	// IMP-8: account sanity check via the account number in columns 2/3
	if err := accountSanityCheck(ctx, q, accountID, res.FileAccounts, &res.Warnings); err != nil {
		return nil, err
	}

	return res, nil
}

// fileAccounts returns the sorted unique account numbers found in columns 2/3.
func fileAccounts(lines []csvparse.RawLine) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range lines {
		for _, a := range []string{l.AccountFrom, l.AccountTo} {
			if a != "" && !seen[a] {
				seen[a] = true
				out = append(out, a)
			}
		}
	}
	sort.Strings(out)
	return out
}

// accountSanityCheck warns when the selected account has an account_number that
// does not appear in any of the file's account columns (IMP-8): "This file may
// belong to a different account". Skipped when the account has no number set.
func accountSanityCheck(ctx context.Context, q *dbq.Queries, accountID int64, fileAccounts []string, warnings *[]Warning) error {
	acc, err := q.GetAccount(ctx, accountID)
	if err != nil {
		return err
	}
	if !acc.AccountNumber.Valid || acc.AccountNumber.String == "" {
		return nil
	}
	want := acc.AccountNumber.String
	for _, a := range fileAccounts {
		if a == want {
			return nil
		}
	}
	*warnings = append(*warnings, Warning{
		Type:    "account_mismatch",
		Message: "This file may belong to a different account: its account number does not match the selected account.",
	})
	return nil
}

// Commit validates the preview token, takes a backup (IMP-10) and inserts the
// non-duplicate lines in a transaction, together with the import batch.
func Commit(ctx context.Context, d *sql.DB, secret []byte, backupDir string, accountID int64, fileName string, content []byte, previewToken string) (*CommitResult, error) {
	sha := SHA256Hex(content)
	if !VerifyPreview(secret, previewToken, accountID, fileName, sha) {
		return nil, ErrInvalidPreviewToken
	}

	parsed, err := csvparse.Parse(bytes.NewReader(content))
	if err != nil {
		return nil, err
	}

	// IMP-10: backup before writing
	if err := db.Backup(ctx, d, backupDir, "pre-import"); err != nil {
		return nil, fmt.Errorf("failed to create backup before import: %w", err)
	}

	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	q := dbq.New(d).WithTx(tx)

	// Re-check duplicates inside the transaction
	var newLines []csvparse.RawLine
	duplicates := 0
	for _, line := range parsed.Lines {
		n, err := q.LineExists(ctx, dbq.LineExistsParams{
			AccountID:   accountID,
			BookingDate: line.Date,
			AmountMinor: line.AmountMinor,
			Message:     line.Message,
			Occurrence:  int64(line.Occurrence),
		})
		if err != nil {
			return nil, err
		}
		if n > 0 {
			duplicates++
			continue
		}
		newLines = append(newLines, line)
	}

	batch, err := q.CreateImportBatch(ctx, dbq.CreateImportBatchParams{
		AccountID:      accountID,
		FileName:       fileName,
		FileSha256:     sha,
		LineCount:      int64(len(newLines)),
		DuplicateCount: int64(duplicates),
	})
	if err != nil {
		return nil, err
	}

	// The imported account's number, used to tell which of the two account
	// columns (from/to) is the counterparty on transfer rows.
	acc, err := q.GetAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}

	newLineIDs := make([]int64, 0, len(newLines))
	for _, line := range newLines {
		id, err := q.InsertStatementLine(ctx, dbq.InsertStatementLineParams{
			AccountID:           accountID,
			ImportID:            sql.NullInt64{Int64: batch.ID, Valid: true},
			BookingDate:         line.Date,
			TransactionDate:     nullString(line.TransactionDate),
			ValueDate:           nullString(line.ValueDate),
			Message:             line.Message,
			AmountMinor:         line.AmountMinor,
			// Column 5 of the CSV is ignored (the bank uses it for something
			// else), so imported lines always get a NULL balance.
			BalanceMinor:        sql.NullInt64{},
			Currency:            line.Currency,
			BankRef:             nullStringOrEmpty(line.BankRef),
			CounterpartyAccount: nullStringOrEmpty(counterpartyOf(line, acc.AccountNumber)),
			FileRow:             int64(line.FileRow),
			Seq:                 int64(line.Seq),
			Occurrence:          int64(line.Occurrence),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to insert line from row %d: %w", line.FileRow, err)
		}
		newLineIDs = append(newLineIDs, id)
	}

	// IMP-11 (M4 D9): run transfer detection over all data inside the import
	// transaction, before rule evaluation, so freshly linked lines
	// (source='transfer') are skipped by the rules engine. Not recorded in the
	// action log (D4: detection is derived state; UNDO-4 covers imports).
	autoPairs, ambiguousGroups, err := transfers.Detect(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("transfer detection failed: %w", err)
	}
	if _, err := transfers.ApplyAutoLinks(ctx, q, autoPairs); err != nil {
		return nil, fmt.Errorf("applying transfer links failed: %w", err)
	}
	transferMatched, transferAmbiguous := len(autoPairs), len(ambiguousGroups)

	// IMP-11 (M3 D11): run rule evaluation on the new lines inside the import
	// transaction. Not recorded in the action log (UNDO-4: the import has its
	// own undo, which deletes the lines and cascades conflict_resolution).
	if _, err := rules.EvaluateLines(ctx, q, newLineIDs); err != nil {
		return nil, fmt.Errorf("rule evaluation failed: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	if err := db.RetainBackups(backupDir, 20); err != nil {
		return nil, err
	}

	// IMP-12: result counts per source within this import batch (after commit,
	// so use the plain handle, not the transaction-bound one)
	qDone := dbq.New(d)
	countBySource := func(source string) int {
		n, err := qDone.CountStatementLines(ctx, dbq.CountStatementLinesParams{
			Source:   source,
			ImportID: sql.NullInt64{Int64: batch.ID, Valid: true},
		})
		if err != nil {
			return 0
		}
		return int(n)
	}

	return &CommitResult{
		ImportID:          batch.ID,
		FileName:          fileName,
		LinesImported:     len(newLines),
		DuplicatesSkipped: duplicates,
		Uncategorized:     countBySource(`["none"]`),
		RuleCategorized:   countBySource(`["rule"]`),
		RuleConflicts:     countBySource(`["conflict"]`),
		TransferMatched:   transferMatched,
		TransferAmbiguous: transferAmbiguous,
		ParseErrors:       parsed.Errors,
	}, nil
}

// counterpartyOf returns the account on the other side of a transfer row: of the
// from/to columns (2/3), the one that is not the imported account. When the
// account has no number set, the to-account (column 3) is used as the fallback.
func counterpartyOf(l csvparse.RawLine, accNum sql.NullString) string {
	from, to := l.AccountFrom, l.AccountTo
	if accNum.Valid {
		if from == accNum.String {
			return to
		}
		if to == accNum.String {
			return from
		}
	}
	if from != "" && to != "" && from != to {
		return to
	}
	return ""
}

// nullString converts an optional string pointer to sql.NullString.
func nullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

// nullStringOrEmpty converts an optional non-pointer string to sql.NullString
// (empty string becomes NULL).
func nullStringOrEmpty(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// UndoLatest deletes the lines of the latest non-undone import batch (globally)
// and marks it undone (IMP-13). A backup is taken first. Transfer links of the
// deleted lines are removed by ON DELETE CASCADE; rejection rows referencing the
// deleted lines are removed explicitly; action-log entries referencing the
// deleted rows are removed too (M4-fix1); the surviving counterpart lines go
// back to rule evaluation.
func UndoLatest(ctx context.Context, d *sql.DB, backupDir string) (int64, error) {
	q := dbq.New(d)
	batch, err := q.GetLatestNonUndoneImport(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNoImportToUndo
		}
		return 0, err
	}

	if err := db.Backup(ctx, d, backupDir, "pre-undo"); err != nil {
		return 0, fmt.Errorf("failed to create backup before undo: %w", err)
	}

	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	qt := dbq.New(d).WithTx(tx)
	importID := sql.NullInt64{Int64: batch.ID, Valid: true}

	// M4 IMP-13: find the links touching this batch's lines before deleting them,
	// so the surviving counterpart can be re-evaluated afterwards.
	links, err := qt.ListTransferLinksByImport(ctx, importID)
	if err != nil {
		return 0, fmt.Errorf("list transfer links by import: %w", err)
	}
	lineIDs, err := qt.ListLineIdsByImport(ctx, importID)
	if err != nil {
		return 0, fmt.Errorf("list line ids by import: %w", err)
	}
	inBatch := map[int64]bool{}
	for _, id := range lineIDs {
		inBatch[id] = true
	}
	var counterpartIDs []int64
	for _, l := range links {
		if inBatch[l.OutLineID] {
			counterpartIDs = append(counterpartIDs, l.InLineID)
		} else {
			counterpartIDs = append(counterpartIDs, l.OutLineID)
		}
	}

	// M4-fix1 (IMP-13, review finding 1): delete the action-log entries that
	// reference rows this undo deletes, so a later action undo/redo cannot
	// resurrect a deleted line or fail on a dangling transfer link/rejection.
	// Runs before the lines are deleted, in the same transaction.
	if err := deleteActionChangesForBatch(ctx, qt, lineIDs, links, inBatch); err != nil {
		return 0, err
	}

	// Rejection rows referencing the deleted lines are cleaned up explicitly
	// (they have no FK); the links themselves cascade with the lines.
	if err := qt.DeleteRejectionsByImport(ctx, importID); err != nil {
		return 0, fmt.Errorf("delete transfer rejections: %w", err)
	}
	if err := qt.DeleteLinesByImport(ctx, importID); err != nil {
		return 0, err
	}
	if err := qt.MarkImportUndone(ctx, batch.ID); err != nil {
		return 0, err
	}

	// The counterpart line (not deleted) loses its link: if it was only marked
	// as transfer, reset it and re-evaluate through the rules engine. A side
	// the user manually recategorized (source='manual', TRF-8) is left alone.
	var reeval []int64
	for _, id := range counterpartIDs {
		row, err := qt.GetLineForAction(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return 0, err
		}
		if row.Source == "transfer" {
			if err := qt.ClearLineTransfer(ctx, id); err != nil {
				return 0, err
			}
			reeval = append(reeval, id)
		}
	}
	if len(reeval) > 0 {
		if _, err := rules.EvaluateLines(ctx, qt, reeval); err != nil {
			return 0, fmt.Errorf("re-evaluate transfer counterparts: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	if err := db.RetainBackups(backupDir, 20); err != nil {
		return 0, err
	}
	return batch.ID, nil
}

// deleteActionChangesForBatch removes the action_change rows that reference
// rows deleted by the import undo (M4-fix1, IMP-13, plan
// docs/plans/M4-fix1-import-undo-action-log.md): statement_line and
// conflict_resolution snapshots of the batch's lines, transfer_link snapshots
// of links touching those lines, and transfer_rejection pairs with either side
// in the batch. Only fully orphaned action_log rows (zero remaining changes)
// are pruned, so actions that also reference surviving rows stay undoable for
// the surviving part. Must run inside UndoLatest's transaction before the
// lines are deleted; line/link ids are already fetched there.
func deleteActionChangesForBatch(ctx context.Context, qt *dbq.Queries, lineIDs []int64, links []*dbq.TransferLink, inBatch map[int64]bool) error {
	idsToJSON := func(ids []int64) (string, error) {
		if ids == nil {
			ids = []int64{}
		}
		b, err := json.Marshal(ids)
		if err != nil {
			return "", fmt.Errorf("marshal ids: %w", err)
		}
		return string(b), nil
	}

	lineIDsJSON, err := idsToJSON(lineIDs)
	if err != nil {
		return err
	}
	if err := qt.DeleteActionChangesForLines(ctx, lineIDsJSON); err != nil {
		return fmt.Errorf("delete action changes for lines: %w", err)
	}

	linkIDs := make([]int64, 0, len(links))
	for _, l := range links {
		linkIDs = append(linkIDs, l.ID)
	}
	linkIDsJSON, err := idsToJSON(linkIDs)
	if err != nil {
		return err
	}
	if err := qt.DeleteActionChangesForLinks(ctx, linkIDsJSON); err != nil {
		return fmt.Errorf("delete action changes for links: %w", err)
	}

	// Rejection pairs use the synthetic entity_id (out*10^8 + in); decode it in
	// Go via the actions package so the factor stays single-sourced (D3).
	rejections, err := qt.ListActionChangesByEntity(ctx, actions.EntityTransferRejection)
	if err != nil {
		return fmt.Errorf("list transfer_rejection action changes: %w", err)
	}
	for _, c := range rejections {
		if actions.RejectionTouchesLines(c.EntityID, inBatch) {
			if err := qt.DeleteActionChange(ctx, dbq.DeleteActionChangeParams{
				ActionID: c.ActionID, Entity: c.Entity, EntityID: c.EntityID,
			}); err != nil {
				return fmt.Errorf("delete transfer_rejection action change: %w", err)
			}
		}
	}

	if err := qt.DeleteOrphanActions(ctx); err != nil {
		return fmt.Errorf("prune orphan actions: %w", err)
	}
	return nil
}
