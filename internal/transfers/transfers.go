// Package transfers implements M4 transfer detection (PRD section 7.4, plan
// docs/plans/M4-transfers.md D2/D3): candidate matching between the user's own
// accounts (different accounts, same booking date, equal and opposite amounts,
// neither linked, pair not rejected), auto-linking of unique 1:1 pairs (TRF-2)
// and ambiguous groups for the review view (TRF-3).
//
// Like the M3 rules engine, detection is pure Go over a SQL-fetched candidate
// set, not complex SQL. Detection runs over all data after every import (TRF-4)
// because the counterpart account's file may be imported later.
package transfers

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mrFred489/financeanalysis/internal/actions"
	"github.com/mrFred489/financeanalysis/internal/db/dbq"
)

// Pair is one candidate transfer: an out-line (amount < 0) and an in-line
// (amount > 0) with equal and opposite amounts.
type Pair struct {
	OutLineID int64 `json:"outLineId"`
	InLineID  int64 `json:"inLineId"`
}

// AmbiguousGroup is one out-line with several candidate in-lines (TRF-3): it is
// never auto-linked; the user picks the correct pair or dismisses the group.
type AmbiguousGroup struct {
	OutLineID          int64   `json:"outLineId"`
	CandidateInLineIDs []int64 `json:"candidateInLineIds"`
}

// candidate is one unlinked statement line that can take part in matching.
type candidate struct {
	id        int64
	accountID int64
	date      string
	amount    int64
}

// pairKey identifies a rejection pair.
type pairKey struct {
	out int64
	in  int64
}

// graphKey groups in-lines by (booking date, amount): an out-line with amount a
// matches in-lines keyed (date, -a).
type graphKey struct {
	date   string
	amount int64
}

// Candidates fetches all lines eligible for matching: not source='transfer',
// non-zero amount, not part of any link (TRF-1).
func Candidates(ctx context.Context, q *dbq.Queries) ([]*dbq.ListTransferCandidatesRow, error) {
	rows, err := q.ListTransferCandidates(ctx)
	if err != nil {
		return nil, fmt.Errorf("list transfer candidates: %w", err)
	}
	return rows, nil
}

// loadRejections returns the set of rejected (out, in) pairs.
func loadRejections(ctx context.Context, q *dbq.Queries) (map[pairKey]bool, error) {
	rows, err := q.ListTransferRejections(ctx)
	if err != nil {
		return nil, fmt.Errorf("list transfer rejections: %w", err)
	}
	set := make(map[pairKey]bool, len(rows))
	for _, r := range rows {
		set[pairKey{out: r.OutLineID, in: r.InLineID}] = true
	}
	return set, nil
}

// buildGraph groups the candidate lines: outs are lines with amount < 0, ins
// with amount > 0, matched on (booking_date, -amount) across different accounts,
// excluding rejected pairs. It returns outCandidates (out-line id -> candidate
// in-line ids) and inCandidateOuts (in-line id -> candidate out-line ids).
func buildGraph(rows []*dbq.ListTransferCandidatesRow, rejected map[pairKey]bool) (outs []candidate, outCandidates map[int64][]int64, inCandidateOuts map[int64][]int64) {
	insByKey := map[graphKey][]candidate{}
	for _, r := range rows {
		c := candidate{id: r.ID, accountID: r.AccountID, date: r.BookingDate, amount: r.AmountMinor}
		if c.amount > 0 {
			k := graphKey{date: c.date, amount: c.amount}
			insByKey[k] = append(insByKey[k], c)
		} else {
			outs = append(outs, c)
		}
	}
	outCandidates = map[int64][]int64{}
	inCandidateOuts = map[int64][]int64{}
	for _, o := range outs {
		k := graphKey{date: o.date, amount: -o.amount}
		for _, in := range insByKey[k] {
			if in.accountID == o.accountID {
				continue // TRF-1: different accounts only
			}
			if rejected[pairKey{out: o.id, in: in.id}] {
				continue // TRF-1: pair was unlinked/dismissed before
			}
			outCandidates[o.id] = append(outCandidates[o.id], in.id)
			inCandidateOuts[in.id] = append(inCandidateOuts[in.id], o.id)
		}
	}
	return outs, outCandidates, inCandidateOuts
}

// Detect scans all unlinked, non-rejected candidate lines and returns the pairs
// to auto-link (unique 1:1, TRF-2) plus the ambiguous groups (TRF-3). A pair is
// auto-linkable only when the out-line has exactly one candidate and that
// candidate has exactly one candidate back (unique 1:1).
func Detect(ctx context.Context, q *dbq.Queries) (auto []Pair, ambiguous []AmbiguousGroup, err error) {
	rows, err := Candidates(ctx, q)
	if err != nil {
		return nil, nil, err
	}
	rejected, err := loadRejections(ctx, q)
	if err != nil {
		return nil, nil, err
	}
	outs, outCandidates, inCandidateOuts := buildGraph(rows, rejected)
	auto = []Pair{}
	ambiguous = []AmbiguousGroup{}
	for _, o := range outs {
		cands := outCandidates[o.id]
		if len(cands) == 0 {
			continue
		}
		if len(cands) == 1 && len(inCandidateOuts[cands[0]]) == 1 {
			auto = append(auto, Pair{OutLineID: o.id, InLineID: cands[0]})
			continue
		}
		ambiguous = append(ambiguous, AmbiguousGroup{OutLineID: o.id, CandidateInLineIDs: cands})
	}
	return auto, ambiguous, nil
}

// CandidatesForOutLine returns the candidate in-line ids for one out-line
// (used by the review/dismiss endpoint).
func CandidatesForOutLine(ctx context.Context, q *dbq.Queries, outLineID int64) ([]int64, error) {
	rows, err := Candidates(ctx, q)
	if err != nil {
		return nil, err
	}
	rejected, err := loadRejections(ctx, q)
	if err != nil {
		return nil, err
	}
	_, outCandidates, _ := buildGraph(rows, rejected)
	return outCandidates[outLineID], nil
}

// ApplyAutoLinks links the given pairs (method='auto') and marks both lines
// source='transfer' with subcategory/rule cleared (TRF-5). It returns one change
// per mutated row; per D4 the caller does NOT record these in the action log
// (detection is derived state) but may use them for counting/testing.
func ApplyAutoLinks(ctx context.Context, q *dbq.Queries, pairs []Pair) ([]actions.Change, error) {
	var changes []actions.Change
	for _, p := range pairs {
		link, err := q.CreateTransferLink(ctx, dbq.CreateTransferLinkParams{
			OutLineID: p.OutLineID, InLineID: p.InLineID, Method: "auto",
		})
		if err != nil {
			return nil, fmt.Errorf("create transfer link %d->%d: %w", p.OutLineID, p.InLineID, err)
		}
		for _, lineID := range []int64{p.OutLineID, p.InLineID} {
			before, err := q.GetLineForAction(ctx, lineID)
			if err != nil {
				return nil, fmt.Errorf("get line %d: %w", lineID, err)
			}
			if err := q.SetLineSourceTransfer(ctx, lineID); err != nil {
				return nil, fmt.Errorf("set line %d to transfer: %w", lineID, err)
			}
			// a linked line is outside the rules engine; drop any remembered resolution
			if err := q.DeleteConflictResolution(ctx, lineID); err != nil && err != sql.ErrNoRows {
				return nil, fmt.Errorf("drop conflict resolution for line %d: %w", lineID, err)
			}
			after, err := q.GetLineForAction(ctx, lineID)
			if err != nil {
				return nil, fmt.Errorf("get line %d after link: %w", lineID, err)
			}
			changes = append(changes, actions.Change{
				Entity: actions.EntityStatementLine, EntityID: lineID, Before: *before, After: *after,
			})
		}
		changes = append(changes, actions.Change{
			Entity: actions.EntityTransferLink, EntityID: link.ID, Before: nil, After: *link,
		})
	}
	return changes, nil
}

// DetectAll runs Detect over all data and applies the auto-links in one
// transaction (TRF-4 on-demand endpoint; IMP-11 import commit uses Detect +
// ApplyAutoLinks inside its own transaction instead). Returns the number of
// auto-linked pairs and ambiguous groups.
func DetectAll(ctx context.Context, d *sql.DB) (autoCount, ambiguousCount int, err error) {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	q := dbq.New(tx)
	auto, ambiguous, err := Detect(ctx, q)
	if err != nil {
		return 0, 0, err
	}
	if _, err := ApplyAutoLinks(ctx, q, auto); err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return len(auto), len(ambiguous), nil
}
