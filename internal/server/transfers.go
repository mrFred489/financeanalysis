package server

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/mrFred489/financeanalysis/internal/actions"
	"github.com/mrFred489/financeanalysis/internal/db/dbq"
	"github.com/mrFred489/financeanalysis/internal/rules"
	"github.com/mrFred489/financeanalysis/internal/transfers"
)

// M4 transfers API (plan docs/plans/M4-transfers.md step 4): list/detect/review
// plus the undoable link/unlink/dismiss mutations. Mutation endpoints follow the
// same pattern as the M2/M3 ones: validate -> begin tx -> snapshot before ->
// mutate -> snapshot after -> actions.Record -> commit.

// transferSideJSON is one side of a transfer link.
type transferSideJSON struct {
	ID          int64  `json:"id"`
	AccountID   int64  `json:"accountId"`
	AccountName string `json:"accountName"`
	BookingDate string `json:"bookingDate"`
	Message     string `json:"message"`
	AmountMinor int64  `json:"amountMinor"`
}

// transferLinkJSON is the API representation of one transfer_link row with both
// lines' details.
type transferLinkJSON struct {
	ID        int64            `json:"id"`
	OutLineID int64            `json:"outLineId"`
	InLineID  int64            `json:"inLineId"`
	Method    string           `json:"method"` // "auto" | "manual"
	CreatedAt string           `json:"createdAt"`
	Out       transferSideJSON `json:"out"`
	In        transferSideJSON `json:"in"`
}

func linkToJSON(row *dbq.ListTransferLinksRow) transferLinkJSON {
	return transferLinkJSON{
		ID: row.ID, OutLineID: row.OutLineID, InLineID: row.InLineID,
		Method: row.Method, CreatedAt: row.CreatedAt,
		Out: transferSideJSON{
			ID: row.OutLineID, AccountID: row.OutAccountID, AccountName: row.OutAccountName,
			BookingDate: row.OutBookingDate, Message: row.OutMessage, AmountMinor: row.OutAmountMinor,
		},
		In: transferSideJSON{
			ID: row.InLineID, AccountID: row.InAccountID, AccountName: row.InAccountName,
			BookingDate: row.InBookingDate, Message: row.InMessage, AmountMinor: row.InAmountMinor,
		},
	}
}

// handleListTransfers: GET /api/transfers — all links with both lines' details.
func (s *Server) handleListTransfers() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		rows, err := dbq.New(d).ListTransferLinks(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		out := make([]transferLinkJSON, 0, len(rows))
		for _, row := range rows {
			out = append(out, linkToJSON(row))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// handleDetectTransfers: POST /api/transfers/detect — run detection over all data
// now (TRF-4) and auto-link the unique 1:1 pairs. Not undoable (derived state, D4).
func (s *Server) handleDetectTransfers() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		auto, ambiguous, err := transfers.DetectAll(r.Context(), d)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"autoLinked": auto, "ambiguous": ambiguous})
	}
}

// reviewGroupJSON is one ambiguous candidate group (TRF-3).
type reviewGroupJSON struct {
	OutLine    lineJSON   `json:"outLine"`
	Candidates []lineJSON `json:"candidates"`
}

// handleTransferReview: GET /api/transfers/review — ambiguous candidate groups.
func (s *Server) handleTransferReview() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		ctx := r.Context()
		q := dbq.New(d)
		_, ambiguous, err := transfers.Detect(ctx, q)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		groups := make([]reviewGroupJSON, 0, len(ambiguous))
		for _, g := range ambiguous {
			outRow, err := q.GetStatementLine(ctx, g.OutLineID)
			if err != nil {
				continue // line vanished between the two queries
			}
			group := reviewGroupJSON{OutLine: lineJSONFromRow(outRow), Candidates: []lineJSON{}}
			for _, inID := range g.CandidateInLineIDs {
				inRow, err := q.GetStatementLine(ctx, inID)
				if err != nil {
					continue
				}
				group.Candidates = append(group.Candidates, lineJSONFromRow(inRow))
			}
			groups = append(groups, group)
		}
		writeJSON(w, http.StatusOK, groups)
	}
}

type linkBody struct {
	OutLineID *int64 `json:"outLineId"`
	InLineID  *int64 `json:"inLineId"`
}

// handleLinkTransfer: POST /api/transfers/link — manual link (TRF-7). Any date is
// allowed, unlike auto-detection; different accounts and exactly opposite
// amounts are required. One undoable action "transfer_link".
func (s *Server) handleLinkTransfer() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		ctx := r.Context()
		var body linkBody
		if err := decodeJSON(r, &body); err != nil || body.OutLineID == nil || body.InLineID == nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_params", Message: "body must be {\"outLineId\": number, \"inLineId\": number}"})
			return
		}
		outID, inID := *body.OutLineID, *body.InLineID
		if outID == inID {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_params", Message: "outLineId and inLineId must differ"})
			return
		}

		tx, err := d.BeginTx(ctx, nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		defer tx.Rollback()
		q := dbq.New(tx)

		outRow, err := q.GetLineForAction(ctx, outID)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, APIError{Code: "not_found", Message: "out line not found"})
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		inRow, err := q.GetLineForAction(ctx, inID)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, APIError{Code: "not_found", Message: "in line not found"})
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		// TRF-7 validation: different accounts, equal and opposite amounts
		if outRow.AccountID == inRow.AccountID {
			writeError(w, http.StatusBadRequest, APIError{Code: "same_account", Message: "Transfer lines must be on different accounts"})
			return
		}
		if outRow.AmountMinor >= 0 || inRow.AmountMinor <= 0 || outRow.AmountMinor != -inRow.AmountMinor {
			writeError(w, http.StatusBadRequest, APIError{Code: "amount_mismatch", Message: "The out line's amount must be the exact opposite of the in line's amount"})
			return
		}
		for _, lineID := range []int64{outID, inID} {
			if _, err := q.GetTransferLinkByLine(ctx, dbq.GetTransferLinkByLineParams{OutLineID: lineID, InLineID: lineID}); err == nil {
				writeError(w, http.StatusConflict, APIError{Code: "already_linked", Message: fmt.Sprintf("Line #%d is already part of a transfer link", lineID)})
				return
			} else if !errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
				return
			}
		}

		link, err := q.CreateTransferLink(ctx, dbq.CreateTransferLinkParams{OutLineID: outID, InLineID: inID, Method: "manual"})
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		changes := []actions.Change{}
		for _, lineID := range []int64{outID, inID} {
			before, err := q.GetLineForAction(ctx, lineID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
				return
			}
			if err := q.SetLineSourceTransfer(ctx, lineID); err != nil {
				writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
				return
			}
			// linked lines are outside the rules engine: drop a remembered resolution
			if crChange, err := clearConflictResolution(ctx, q, lineID); err != nil {
				writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
				return
			} else if crChange != nil {
				changes = append(changes, *crChange)
			}
			after, err := q.GetLineForAction(ctx, lineID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
				return
			}
			changes = append(changes, actions.Change{Entity: actions.EntityStatementLine, EntityID: lineID, Before: *before, After: *after})
		}
		changes = append(changes, actions.Change{Entity: actions.EntityTransferLink, EntityID: link.ID, Before: nil, After: *link})
		desc := fmt.Sprintf("Linked transfer #%d → #%d", outID, inID)
		if _, err := actions.Record(ctx, tx, "transfer_link", desc, changes); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		if err := tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}

		rows, err := dbq.New(d).ListTransferLinks(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		for _, row := range rows {
			if row.ID == link.ID {
				writeJSON(w, http.StatusCreated, linkToJSON(row))
				return
			}
		}
		// fallback (should not happen): the row was just committed
		writeJSON(w, http.StatusCreated, transferLinkJSON{
			ID: link.ID, OutLineID: outID, InLineID: inID, Method: link.Method, CreatedAt: link.CreatedAt,
			Out: transferSideJSON{ID: outID},
			In:  transferSideJSON{ID: inID},
		})
	}
}

// handleUnlinkTransfer: POST /api/transfers/{id}/unlink — TRF-6. Deletes the link,
// records the rejection pair, resets both lines to source='none' and re-evaluates
// them through the rules engine. One undoable action "transfer_unlink".
func (s *Server) handleUnlinkTransfer() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		ctx := r.Context()
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_id", Message: "invalid transfer id"})
			return
		}

		tx, err := d.BeginTx(ctx, nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		defer tx.Rollback()
		q := dbq.New(tx)

		link, err := q.GetTransferLink(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, APIError{Code: "not_found", Message: "transfer link not found"})
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		outID, inID := link.OutLineID, link.InLineID

		outBefore, err := q.GetLineForAction(ctx, outID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		inBefore, err := q.GetLineForAction(ctx, inID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}

		if err := q.DeleteTransferLink(ctx, id); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		if err := q.InsertTransferRejection(ctx, dbq.InsertTransferRejectionParams{OutLineID: outID, InLineID: inID}); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		rejection, err := q.GetTransferRejection(ctx, dbq.GetTransferRejectionParams{OutLineID: outID, InLineID: inID})
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		// back to candidate state, then rules re-evaluate both lines (TRF-6)
		if err := q.ClearLineTransfer(ctx, outID); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		if err := q.ClearLineTransfer(ctx, inID); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		if _, err := rules.EvaluateLines(ctx, q, []int64{outID, inID}); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		outAfter, err := q.GetLineForAction(ctx, outID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		inAfter, err := q.GetLineForAction(ctx, inID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}

		changes := []actions.Change{
			{Entity: actions.EntityStatementLine, EntityID: outID, Before: *outBefore, After: *outAfter},
			{Entity: actions.EntityStatementLine, EntityID: inID, Before: *inBefore, After: *inAfter},
			{Entity: actions.EntityTransferLink, EntityID: id, Before: *link, After: nil},
			{Entity: actions.EntityTransferRejection, EntityID: actions.EncodeRejectionID(outID, inID), Before: nil, After: *rejection},
		}
		desc := fmt.Sprintf("Unlinked transfer #%d ↔ #%d", outID, inID)
		if _, err := actions.Record(ctx, tx, "transfer_unlink", desc, changes); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		if err := tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}

		qd := dbq.New(d)
		outRow, err := qd.GetStatementLine(ctx, outID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		inRow, err := qd.GetStatementLine(ctx, inID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "unlinked", "linkId": id,
			"lines": []lineJSON{lineJSONFromRow(outRow), lineJSONFromRow(inRow)},
		})
	}
}

type dismissBody struct {
	OutLineID *int64 `json:"outLineId"`
}

// handleDismissReview: POST /api/transfers/review/dismiss — dismiss an ambiguous
// group (TRF-3): insert a rejection pair for every candidate combination of the
// out-line, so they are never auto-matched again. One undoable action
// "transfer_dismiss".
func (s *Server) handleDismissReview() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		ctx := r.Context()
		var body dismissBody
		if err := decodeJSON(r, &body); err != nil || body.OutLineID == nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_params", Message: "body must be {\"outLineId\": number}"})
			return
		}
		outID := *body.OutLineID

		tx, err := d.BeginTx(ctx, nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		defer tx.Rollback()
		q := dbq.New(tx)

		if _, err := q.GetLineForAction(ctx, outID); errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, APIError{Code: "not_found", Message: "out line not found"})
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		candidates, err := transfers.CandidatesForOutLine(ctx, q, outID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		if len(candidates) == 0 {
			writeError(w, http.StatusConflict, APIError{Code: "nothing_to_dismiss", Message: "This line has no ambiguous candidates"})
			return
		}
		changes := make([]actions.Change, 0, len(candidates))
		for _, inID := range candidates {
			if err := q.InsertTransferRejection(ctx, dbq.InsertTransferRejectionParams{OutLineID: outID, InLineID: inID}); err != nil {
				writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
				return
			}
			rej, err := q.GetTransferRejection(ctx, dbq.GetTransferRejectionParams{OutLineID: outID, InLineID: inID})
			if err != nil {
				writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
				return
			}
			changes = append(changes, actions.Change{
				Entity: actions.EntityTransferRejection, EntityID: actions.EncodeRejectionID(outID, inID),
				Before: nil, After: *rej,
			})
		}
		desc := fmt.Sprintf("Dismissed %d transfer candidate(s) for line #%d", len(candidates), outID)
		if _, err := actions.Record(ctx, tx, "transfer_dismiss", desc, changes); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		if err := tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "dismissed", "outLineId": outID, "dismissed": len(candidates)})
	}
}

// handleGetLineTransfer: GET /api/lines/{id}/transfer — the link and counterpart
// line of a given line (used by the statements/categorize UI). Nulls when the
// line is not linked.
func (s *Server) handleGetLineTransfer() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		ctx := r.Context()
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_id", Message: "invalid line id"})
			return
		}
		q := dbq.New(d)
		link, err := q.GetTransferLinkByLine(ctx, dbq.GetTransferLinkByLineParams{OutLineID: id, InLineID: id})
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusOK, map[string]any{"link": nil, "counterpart": nil})
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		counterpartID := link.InLineID
		if link.OutLineID != id {
			counterpartID = link.OutLineID
		}
		counterpart, err := q.GetStatementLine(ctx, counterpartID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"link":        link,
			"counterpart": lineJSONFromRow(counterpart),
		})
	}
}
