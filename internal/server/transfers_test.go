package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// M4 integration tests (plan docs/plans/M4-transfers.md step 4/8/10): the
// transfer endpoints over httptest + temp SQLite, including import-triggered
// detection, import undo cascade and undo of link/unlink/dismiss.

// setupTwoAccounts creates a database with two accounts and returns their ids.
func setupTwoAccounts(t *testing.T, ts *httptest.Server) (int64, int64) {
	t.Helper()
	mainID := createDBViaAPI(t, ts)
	status, body, _ := doJSON(t, ts, "POST", "/api/accounts", map[string]any{"name": "Savings", "account_number": "9999 0000000002"})
	if status != http.StatusCreated {
		t.Fatalf("create savings account: %d %v", status, body)
	}
	return mainID, int64(body["id"].(float64))
}

// importCSV runs preview+commit for one CSV file and returns the commit body.
func importCSV(t *testing.T, ts *httptest.Server, accountID int64, name string, content []byte) map[string]any {
	t.Helper()
	buf, ct := multipartBody(t, map[string]string{"accountId": fmt.Sprint(accountID)}, "file", name, content)
	status, body, _ := doMultipart(t, ts, "POST", "/api/imports/preview", buf, ct)
	if status != http.StatusOK {
		t.Fatalf("preview %s: %d %v", name, status, body)
	}
	token, _ := body["previewToken"].(string)
	buf, ct = multipartBody(t, map[string]string{"accountId": fmt.Sprint(accountID), "previewToken": token}, "file", name, content)
	status, body, _ = doMultipart(t, ts, "POST", "/api/imports", buf, ct)
	if status != http.StatusCreated {
		t.Fatalf("commit %s: %d %v", name, status, body)
	}
	return body
}

type transferLinkAPIRow struct {
	ID        int64  `json:"id"`
	OutLineID int64  `json:"outLineId"`
	InLineID  int64  `json:"inLineId"`
	Method    string `json:"method"`
	Out       struct {
		ID          int64  `json:"id"`
		AccountName string `json:"accountName"`
		AmountMinor int64  `json:"amountMinor"`
	} `json:"out"`
	In struct {
		ID          int64  `json:"id"`
		AccountName string `json:"accountName"`
		AmountMinor int64  `json:"amountMinor"`
	} `json:"in"`
}

func listTransfers(t *testing.T, ts *httptest.Server) []transferLinkAPIRow {
	t.Helper()
	status, _, raw := doJSON(t, ts, "GET", "/api/transfers", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/transfers: %d", status)
	}
	var out []transferLinkAPIRow
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode transfers: %v (%s)", err, raw)
	}
	return out
}

func getLineAPI(t *testing.T, ts *httptest.Server, id int64) map[string]any {
	t.Helper()
	status, body, _ := doJSON(t, ts, "GET", fmt.Sprintf("/api/lines/%d", id), nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/lines/%d: %d %v", id, status, body)
	}
	return body
}

// linesOfAccount returns all lines of one account via the API.
func linesOfAccount(t *testing.T, ts *httptest.Server, accountID int64) []map[string]any {
	t.Helper()
	status, _, raw := doJSON(t, ts, "GET", fmt.Sprintf("/api/lines?pageSize=100&accounts=%d", accountID), nil)
	if status != http.StatusOK {
		t.Fatalf("GET lines: %d", status)
	}
	var lr struct {
		Lines []map[string]any `json:"lines"`
	}
	if err := json.Unmarshal(raw, &lr); err != nil {
		t.Fatalf("decode lines: %v", err)
	}
	return lr.Lines
}

func findLine(t *testing.T, ts *httptest.Server, accountID int64, message string) map[string]any {
	t.Helper()
	for _, l := range linesOfAccount(t, ts, accountID) {
		if l["message"] == message {
			return l
		}
	}
	t.Fatalf("line with message %q not found on account %d", message, accountID)
	return nil
}

// TestTransferImportDetectionReviewDismiss: importing both account files links
// the unique pair automatically, reports the ambiguous group, and dismiss makes
// detection stop suggesting it. Undo of the dismiss restores the suggestions.
func TestTransferImportDetectionReviewDismiss(t *testing.T) {
	ts := newTestServer(t)
	mainID, savingsID := setupTwoAccounts(t, ts)

	// First import (main account): no counterpart yet, nothing matched.
	body := importCSV(t, ts, mainID, "transfer_main.csv", fixtureCSV(t, "transfer_main.csv"))
	if body["transferMatched"] != float64(0) || body["transferAmbiguous"] != float64(0) {
		t.Fatalf("first import counts: %v", body)
	}
	if links := listTransfers(t, ts); len(links) != 0 {
		t.Fatalf("expected no links yet, got %+v", links)
	}

	// Second import (savings account): detection runs over all data (TRF-4),
	// links the unique 4000 pair and reports the two ambiguous groups.
	body = importCSV(t, ts, savingsID, "transfer_savings.csv", fixtureCSV(t, "transfer_savings.csv"))
	if body["transferMatched"] != float64(1) {
		t.Fatalf("expected transferMatched=1, got %v", body["transferMatched"])
	}
	if body["transferAmbiguous"] != float64(2) {
		t.Fatalf("expected transferAmbiguous=2, got %v", body["transferAmbiguous"])
	}

	links := listTransfers(t, ts)
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %+v", links)
	}
	link := links[0]
	if link.Method != "auto" || link.Out.AmountMinor != -400000 || link.In.AmountMinor != 400000 {
		t.Fatalf("unexpected link: %+v", link)
	}
	if link.Out.AccountName != "Main account" || link.In.AccountName != "Savings" {
		t.Fatalf("unexpected accounts: %+v", link)
	}

	// The linked lines carry source=transfer and the transfer fields (D11).
	outLine := findLine(t, ts, mainID, "Transfer to savings")
	inLine := findLine(t, ts, savingsID, "Transfer from main")
	if outLine["source"] != "transfer" || inLine["source"] != "transfer" {
		t.Fatalf("linked lines must have source=transfer, got %v / %v", outLine["source"], inLine["source"])
	}
	if outLine["transferLinkId"] != float64(link.ID) || outLine["transferCounterpartId"] != inLine["id"] {
		t.Fatalf("out line transfer fields wrong: %v %v", outLine["transferLinkId"], outLine["transferCounterpartId"])
	}
	if inLine["transferLinkId"] != float64(link.ID) || inLine["transferCounterpartId"] != outLine["id"] {
		t.Fatalf("in line transfer fields wrong: %v %v", inLine["transferLinkId"], inLine["transferCounterpartId"])
	}

	// GET /api/lines/{id}/transfer returns link + counterpart.
	status, body, _ := doJSON(t, ts, "GET", fmt.Sprintf("/api/lines/%d/transfer", int64(outLine["id"].(float64))), nil)
	if status != http.StatusOK || body["link"] == nil || body["counterpart"] == nil {
		t.Fatalf("line transfer endpoint: %d %v", status, body)
	}

	// Review: two ambiguous groups, each with two candidates.
	status, _, raw := doJSON(t, ts, "GET", "/api/transfers/review", nil)
	if status != http.StatusOK {
		t.Fatalf("review: %d", status)
	}
	var groups []struct {
		OutLine struct {
			ID      int64  `json:"id"`
			Message string `json:"message"`
		} `json:"outLine"`
		Candidates []struct {
			ID int64 `json:"id"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(raw, &groups); err != nil {
		t.Fatalf("decode review: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 review groups, got %+v", groups)
	}
	var outAmbID int64
	for _, g := range groups {
		if g.OutLine.Message == "Transfer ambiguous" {
			outAmbID = g.OutLine.ID
			if len(g.Candidates) != 2 {
				t.Fatalf("expected 2 candidates in the 1000 group, got %+v", g.Candidates)
			}
		}
	}
	if outAmbID == 0 {
		t.Fatalf("1000 ambiguous group not found in %+v", groups)
	}

	// On-demand detection does not re-link anything (idempotent).
	status, body, _ = doJSON(t, ts, "POST", "/api/transfers/detect", nil)
	if status != http.StatusOK || body["autoLinked"] != float64(0) || body["ambiguous"] != float64(2) {
		t.Fatalf("detect: %d %v", status, body)
	}

	// Dismiss the 1000 group; detection must not suggest it again (the other
	// group remains).
	status, body, _ = doJSON(t, ts, "POST", "/api/transfers/review/dismiss", map[string]any{"outLineId": outAmbID})
	if status != http.StatusOK || body["dismissed"] != float64(2) {
		t.Fatalf("dismiss: %d %v", status, body)
	}
	_, _, raw = doJSON(t, ts, "GET", "/api/transfers/review", nil)
	groups = nil
	json.Unmarshal(raw, &groups)
	if len(groups) != 1 || groups[0].OutLine.Message != "Transfer ambiguous 2" {
		t.Fatalf("expected only the 2000 group left after dismiss, got %+v", groups)
	}
	_, body, _ = doJSON(t, ts, "POST", "/api/transfers/detect", nil)
	if body["autoLinked"] != float64(0) || body["ambiguous"] != float64(1) {
		t.Fatalf("dismissed group must not be re-suggested, got %v", body)
	}

	// Undo the dismiss -> both groups are suggested again.
	status, body, _ = doJSON(t, ts, "POST", "/api/actions/undo", nil)
	if status != http.StatusOK || body["kind"] != "transfer_dismiss" {
		t.Fatalf("undo dismiss: %d %v", status, body)
	}
	_, _, raw = doJSON(t, ts, "GET", "/api/transfers/review", nil)
	groups = nil
	json.Unmarshal(raw, &groups)
	if len(groups) != 2 {
		t.Fatalf("undo of dismiss must restore both review groups, got %+v", groups)
	}
}

// TestTransferManualLinkUnlinkUndo: validation errors (400/409), manual link,
// TRF-8 recategorization keeps the link, unlink records the rejection and
// re-evaluates, undo restores the link.
func TestTransferManualLinkUnlinkUndo(t *testing.T) {
	ts := newTestServer(t)
	mainID, savingsID := setupTwoAccounts(t, ts)
	importCSV(t, ts, mainID, "transfer_main.csv", fixtureCSV(t, "transfer_main.csv"))
	importCSV(t, ts, savingsID, "transfer_savings.csv", fixtureCSV(t, "transfer_savings.csv"))

	outAmb := findLine(t, ts, mainID, "Transfer ambiguous")
	ambA := findLine(t, ts, savingsID, "Transfer ambiguous A")
	coffee := findLine(t, ts, mainID, "Coffee shop")
	outID := int64(outAmb["id"].(float64))
	inID := int64(ambA["id"].(float64))
	coffeeID := int64(coffee["id"].(float64))

	// Validation: same account -> 400
	status, body, _ := doJSON(t, ts, "POST", "/api/transfers/link", map[string]any{"outLineId": outID, "inLineId": coffeeID})
	if status != http.StatusBadRequest || body["error"].(map[string]any)["code"] != "same_account" {
		t.Fatalf("same account: %d %v", status, body)
	}
	// Validation: not opposite amounts -> 400
	status, body, _ = doJSON(t, ts, "POST", "/api/transfers/link", map[string]any{"outLineId": outID, "inLineId": int64(findLine(t, ts, savingsID, "Transfer from main")["id"].(float64))})
	if status != http.StatusBadRequest || body["error"].(map[string]any)["code"] != "amount_mismatch" {
		t.Fatalf("amount mismatch: %d %v", status, body)
	}

	// Manual link of the ambiguous pair (TRF-7, any date allowed).
	status, body, _ = doJSON(t, ts, "POST", "/api/transfers/link", map[string]any{"outLineId": outID, "inLineId": inID})
	if status != http.StatusCreated || body["method"] != "manual" {
		t.Fatalf("manual link: %d %v", status, body)
	}
	linkID := int64(body["id"].(float64))

	// Already linked -> 409
	status, body, _ = doJSON(t, ts, "POST", "/api/transfers/link", map[string]any{"outLineId": outID, "inLineId": int64(findLine(t, ts, savingsID, "Transfer ambiguous B")["id"].(float64))})
	if status != http.StatusConflict || body["error"].(map[string]any)["code"] != "already_linked" {
		t.Fatalf("already linked: %d %v", status, body)
	}

	// Both lines are transfer now.
	if l := getLineAPI(t, ts, outID); l["source"] != "transfer" || l["transferLinkId"] != float64(linkID) {
		t.Fatalf("linked out line: %v", l)
	}

	// TRF-8: manual recategorization of one side keeps the link.
	status, _, raw := doJSON(t, ts, "GET", "/api/subcategories", nil)
	if status != http.StatusOK {
		t.Fatalf("subcategories: %d", status)
	}
	var subs []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	json.Unmarshal(raw, &subs)
	if len(subs) == 0 {
		t.Fatalf("no subcategories available")
	}
	savingsTransferSub := subs[0].ID
	status, body, _ = doJSON(t, ts, "POST", fmt.Sprintf("/api/lines/%d/categorize", outID), map[string]any{"subcategoryId": savingsTransferSub})
	if status != http.StatusOK {
		t.Fatalf("categorize linked line: %d %v", status, body)
	}
	if body["source"] != "manual" || body["transferLinkId"] != float64(linkID) {
		t.Fatalf("TRF-8: link must remain after manual categorization, got %v", body)
	}

	// Unlink (TRF-6): link deleted, rejection recorded, lines re-evaluated.
	status, body, _ = doJSON(t, ts, "POST", fmt.Sprintf("/api/transfers/%d/unlink", linkID), nil)
	if status != http.StatusOK || body["status"] != "unlinked" {
		t.Fatalf("unlink: %d %v", status, body)
	}
	lines := body["lines"].([]any)
	for _, raw := range lines {
		l := raw.(map[string]any)
		if l["source"] != "none" {
			t.Fatalf("after unlink lines must be re-evaluated (none, no rules), got %v", l["source"])
		}
	}
	if links := listTransfers(t, ts); len(links) != 1 { // only the auto 4000 link remains
		t.Fatalf("expected only the auto link left, got %+v", links)
	}

	// Undo the unlink immediately: link restored, rejection removed, line
	// sources restored (the out line goes back to its manual categorization).
	status, body, _ = doJSON(t, ts, "POST", "/api/actions/undo", nil)
	if status != http.StatusOK || body["kind"] != "transfer_unlink" {
		t.Fatalf("undo unlink: %d %v", status, body)
	}
	links := listTransfers(t, ts)
	if len(links) != 2 {
		t.Fatalf("undo must restore the manual link, got %+v", links)
	}
	l := getLineAPI(t, ts, outID)
	if l["source"] != "manual" || l["transferLinkId"] != float64(linkID) {
		t.Fatalf("undo must restore the manual source + link, got %v", l)
	}
	// Detection is idempotent while the pair is linked.
	_, body, _ = doJSON(t, ts, "POST", "/api/transfers/detect", nil)
	if body["autoLinked"] != float64(0) {
		t.Fatalf("linked pair must not be re-linked, got %v", body)
	}
}

// TestTransferUnlinkRecordsRejection: after unlinking the auto-detected pair, the
// exact pair is rejected and detection never suggests it again; undo brings the
// link back.
func TestTransferUnlinkRecordsRejection(t *testing.T) {
	ts := newTestServer(t)
	mainID, savingsID := setupTwoAccounts(t, ts)
	importCSV(t, ts, mainID, "transfer_main.csv", fixtureCSV(t, "transfer_main.csv"))
	importCSV(t, ts, savingsID, "transfer_savings.csv", fixtureCSV(t, "transfer_savings.csv"))

	links := listTransfers(t, ts)
	if len(links) != 1 {
		t.Fatalf("expected 1 auto link, got %+v", links)
	}
	linkID := links[0].ID

	status, body, _ := doJSON(t, ts, "POST", fmt.Sprintf("/api/transfers/%d/unlink", linkID), nil)
	if status != http.StatusOK {
		t.Fatalf("unlink: %d %v", status, body)
	}
	// The unlinked pair must not be auto-linked again (rejection). The
	// unrelated ambiguous 1000 group is still reported, which is fine.
	_, body, _ = doJSON(t, ts, "POST", "/api/transfers/detect", nil)
	if body["autoLinked"] != float64(0) {
		t.Fatalf("rejected pair must not be re-linked, got %v", body)
	}
	if links := listTransfers(t, ts); len(links) != 0 {
		t.Fatalf("no link must exist after unlink, got %+v", links)
	}

	// Undo restores the link and removes the rejection.
	status, body, _ = doJSON(t, ts, "POST", "/api/actions/undo", nil)
	if status != http.StatusOK || body["kind"] != "transfer_unlink" {
		t.Fatalf("undo: %d %v", status, body)
	}
	if links := listTransfers(t, ts); len(links) != 1 || links[0].ID != linkID {
		t.Fatalf("undo must restore the link, got %+v", links)
	}
	_, body, _ = doJSON(t, ts, "POST", "/api/transfers/detect", nil)
	if body["autoLinked"] != float64(0) {
		t.Fatalf("restored link must not be re-linked, got %v", body)
	}
}

// TestTransferImportUndoCascades: undoing the import that carried one side of a
// transfer removes the link (ON DELETE CASCADE), cleans rejections and resets
// the surviving counterpart to rule evaluation (IMP-13).
func TestTransferImportUndoCascades(t *testing.T) {
	ts := newTestServer(t)
	mainID, savingsID := setupTwoAccounts(t, ts)
	importCSV(t, ts, mainID, "transfer_main.csv", fixtureCSV(t, "transfer_main.csv"))
	body := importCSV(t, ts, savingsID, "transfer_savings.csv", fixtureCSV(t, "transfer_savings.csv"))
	if body["transferMatched"] != float64(1) {
		t.Fatalf("expected 1 matched transfer, got %v", body)
	}
	importID := int64(body["importId"].(float64))
	if links := listTransfers(t, ts); len(links) != 1 {
		t.Fatalf("expected 1 link before undo, got %+v", links)
	}

	outLine := findLine(t, ts, mainID, "Transfer to savings")

	// Undo the savings import.
	status, _, _ := doJSON(t, ts, "POST", fmt.Sprintf("/api/imports/%d/undo", importID), nil)
	if status != http.StatusOK {
		t.Fatalf("import undo: %d", status)
	}

	// The link is gone (cascade), and the surviving counterpart is back to none.
	if links := listTransfers(t, ts); len(links) != 0 {
		t.Fatalf("import undo must remove the link, got %+v", links)
	}
	l := getLineAPI(t, ts, int64(outLine["id"].(float64)))
	if l["source"] != "none" || l["transferLinkId"] != nil {
		t.Fatalf("counterpart must be reset to none and unlinked, got source=%v link=%v", l["source"], l["transferLinkId"])
	}

	// Detection now finds nothing (the savings lines are deleted).
	_, body, _ = doJSON(t, ts, "POST", "/api/transfers/detect", nil)
	if body["autoLinked"] != float64(0) {
		t.Fatalf("nothing to link after undo, got %v", body)
	}
}
