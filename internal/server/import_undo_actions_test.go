package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mrFred489/financeanalysis/internal/db"
)

// M4-fix1 integration tests (plan docs/plans/M4-fix1-import-undo-action-log.md,
// review finding 1 / IMP-13): undoing an import must also clean up action-log
// entries referencing the deleted rows, so a later action undo/redo can neither
// resurrect a deleted statement line nor fail on dangling transfer rows.

type actionAPIRow struct {
	ID          int64   `json:"id"`
	Kind        string  `json:"kind"`
	Description string  `json:"description"`
	ChangeCount int64   `json:"changeCount"`
	UndoneAt    *string `json:"undoneAt"`
}

func listActions(t *testing.T, ts *httptest.Server) []actionAPIRow {
	t.Helper()
	status, _, raw := doJSON(t, ts, "GET", "/api/actions", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/actions: %d", status)
	}
	var out []actionAPIRow
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode actions: %v (%s)", err, raw)
	}
	return out
}

func firstSubcategoryID(t *testing.T, ts *httptest.Server) int64 {
	t.Helper()
	status, _, raw := doJSON(t, ts, "GET", "/api/subcategories", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/subcategories: %d", status)
	}
	var subs []struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(raw, &subs); err != nil || len(subs) == 0 {
		t.Fatalf("decode subcategories: %v (%s)", err, raw)
	}
	return subs[0].ID
}

func totalLines(t *testing.T, ts *httptest.Server) int64 {
	t.Helper()
	status, _, raw := doJSON(t, ts, "GET", "/api/lines?pageSize=1", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/lines: %d", status)
	}
	var lr struct {
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(raw, &lr); err != nil {
		t.Fatalf("decode lines: %v (%s)", err, raw)
	}
	return lr.Total
}

func actionLogCount(t *testing.T) int64 {
	t.Helper()
	d, err := db.GetDB()
	if err != nil {
		t.Fatalf("GetDB: %v", err)
	}
	var n int64
	if err := d.QueryRow("SELECT COUNT(*) FROM action_log").Scan(&n); err != nil {
		t.Fatalf("count action_log: %v", err)
	}
	return n
}

func actionChangeCount(t *testing.T, entity string) int64 {
	t.Helper()
	d, err := db.GetDB()
	if err != nil {
		t.Fatalf("GetDB: %v", err)
	}
	var n int64
	if err := d.QueryRow("SELECT COUNT(*) FROM action_change WHERE entity = ?", entity).Scan(&n); err != nil {
		t.Fatalf("count action_change: %v", err)
	}
	return n
}

// T1: categorize-then-undo-import. The categorize action referenced only the
// deleted line, so it is pruned from the action log; a later action undo must
// succeed on the surviving action and must not resurrect the line.
func TestImportUndoCleansCategorizeAction(t *testing.T) {
	ts := newTestServer(t)
	accountID := createDBViaAPI(t, ts)
	body := importCSV(t, ts, accountID, "normal.csv", fixtureCSV(t, "normal.csv"))
	importID := int64(body["importId"].(float64))

	line := findLine(t, ts, accountID, "Køb i Netto")
	lineID := int64(line["id"].(float64))
	subID := firstSubcategoryID(t, ts)

	status, cbody, _ := doJSON(t, ts, "POST", fmt.Sprintf("/api/lines/%d/categorize", lineID), map[string]any{"subcategoryId": subID})
	if status != http.StatusOK {
		t.Fatalf("categorize: %d %v", status, cbody)
	}
	// A second action (rule create) so the undo stack still has a surviving entry
	status, rbody, _ := doJSON(t, ts, "POST", "/api/rules", map[string]any{
		"name": "Netto", "pattern": "Netto", "direction": "both", "subcategoryId": subID,
	})
	if status != http.StatusCreated {
		t.Fatalf("create rule: %d %v", status, rbody)
	}

	if actions := listActions(t, ts); len(actions) != 2 {
		t.Fatalf("expected 2 actions, got %+v", actions)
	}
	logCountBefore := actionLogCount(t)

	// Undo the import carrying the categorized line
	status, _, _ = doJSON(t, ts, "POST", fmt.Sprintf("/api/imports/%d/undo", importID), nil)
	if status != http.StatusOK {
		t.Fatalf("undo import: %d", status)
	}
	if total := totalLines(t, ts); total != 0 {
		t.Errorf("expected 0 lines after import undo, got %d", total)
	}
	status, _, _ = doJSON(t, ts, "GET", fmt.Sprintf("/api/lines/%d", lineID), nil)
	if status != http.StatusNotFound {
		t.Errorf("expected 404 for the deleted line, got %d", status)
	}

	// The categorize action is no longer listed (its only change referenced the
	// deleted line); the rule action survives.
	actionsAfter := listActions(t, ts)
	if len(actionsAfter) != 1 || actionsAfter[0].Kind != "rule_create" {
		t.Errorf("expected only the rule_create action left, got %+v", actionsAfter)
	}
	// T5: the orphaned action_log row was pruned (does not consume a Ctrl+Z)
	if n := actionLogCount(t); n != logCountBefore-1 {
		t.Errorf("expected action_log count %d after undo, got %d", logCountBefore-1, n)
	}

	// Ctrl+Z undoes the surviving action and must not resurrect the line
	status, ubody, _ := doJSON(t, ts, "POST", "/api/actions/undo", nil)
	if status != http.StatusOK || ubody["kind"] != "rule_create" {
		t.Fatalf("actions undo: %d %v", status, ubody)
	}
	if total := totalLines(t, ts); total != 0 {
		t.Errorf("action undo must not resurrect the deleted line, total=%d", total)
	}
	status, _, _ = doJSON(t, ts, "GET", fmt.Sprintf("/api/lines/%d", lineID), nil)
	if status != http.StatusNotFound {
		t.Errorf("deleted line must stay deleted after action undo, got %d", status)
	}
	status, _, _ = doJSON(t, ts, "POST", "/api/actions/undo", nil)
	if status != http.StatusConflict {
		t.Errorf("expected 409 nothing_to_undo, got %d", status)
	}
}

// T2: manual transfer link + undo import. The link cascades with the deleted
// lines; the transfer_link action keeps only the change of the surviving line,
// and undoing it never re-inserts a link for deleted lines.
func TestImportUndoCleansTransferLinkAction(t *testing.T) {
	ts := newTestServer(t)
	mainID, savingsID := setupTwoAccounts(t, ts)
	importCSV(t, ts, mainID, "transfer_main.csv", fixtureCSV(t, "transfer_main.csv"))
	body := importCSV(t, ts, savingsID, "transfer_savings.csv", fixtureCSV(t, "transfer_savings.csv"))
	savingsImportID := int64(body["importId"].(float64))
	if body["transferMatched"] != float64(1) {
		t.Fatalf("expected 1 auto-matched transfer, got %v", body)
	}

	// Manually link an ambiguous pair (out on main, in on savings)
	outLine := findLine(t, ts, mainID, "Transfer ambiguous")
	inLine := findLine(t, ts, savingsID, "Transfer ambiguous A")
	outID := int64(outLine["id"].(float64))
	inID := int64(inLine["id"].(float64))
	status, lbody, _ := doJSON(t, ts, "POST", "/api/transfers/link", map[string]any{"outLineId": outID, "inLineId": inID})
	if status != http.StatusCreated {
		t.Fatalf("manual link: %d %v", status, lbody)
	}

	before := listActions(t, ts)
	if len(before) != 1 || before[0].Kind != "transfer_link" || before[0].ChangeCount != 3 {
		t.Fatalf("expected one transfer_link action with 3 changes, got %+v", before)
	}

	// Undo the savings import (carries the in side of the manual link)
	status, _, _ = doJSON(t, ts, "POST", fmt.Sprintf("/api/imports/%d/undo", savingsImportID), nil)
	if status != http.StatusOK {
		t.Fatalf("undo import: %d", status)
	}
	if links := listTransfers(t, ts); len(links) != 0 {
		t.Fatalf("links must cascade with the deleted lines, got %+v", links)
	}

	// The transfer_link action survives with only the surviving line's change:
	// the in-line statement_line change and the transfer_link change referenced
	// rows deleted by the undo and were cleaned.
	after := listActions(t, ts)
	if len(after) != 1 || after[0].Kind != "transfer_link" {
		t.Fatalf("expected the transfer_link action to survive, got %+v", after)
	}
	if after[0].ChangeCount != 1 {
		t.Errorf("expected changeCount 1 (surviving line only), got %d", after[0].ChangeCount)
	}
	if n := actionChangeCount(t, "transfer_link"); n != 0 {
		t.Errorf("expected 0 transfer_link change rows, got %d", n)
	}

	// Undo the remaining action: 200, and it must not re-insert the link
	status, ubody, _ := doJSON(t, ts, "POST", "/api/actions/undo", nil)
	if status != http.StatusOK || ubody["kind"] != "transfer_link" {
		t.Fatalf("actions undo: %d %v", status, ubody)
	}
	if links := listTransfers(t, ts); len(links) != 0 {
		t.Errorf("undo must not re-insert a link for deleted lines, got %+v", links)
	}
	l := getLineAPI(t, ts, outID)
	if l["source"] == "transfer" || l["transferLinkId"] != nil {
		t.Errorf("surviving line must be restored to its pre-link state, got source=%v link=%v", l["source"], l["transferLinkId"])
	}
	status, _, _ = doJSON(t, ts, "GET", fmt.Sprintf("/api/lines/%d", inID), nil)
	if status != http.StatusNotFound {
		t.Errorf("deleted in-line must stay deleted, got %d", status)
	}
}

// T3: unlink-then-undo-import. The transfer_unlink action records a
// transfer_rejection change with the synthetic pair id; undoing an import
// carrying either side of the pair cleans that change row.
func TestImportUndoCleansRejectionAction(t *testing.T) {
	ts := newTestServer(t)
	mainID, savingsID := setupTwoAccounts(t, ts)
	importCSV(t, ts, mainID, "transfer_main.csv", fixtureCSV(t, "transfer_main.csv"))
	body := importCSV(t, ts, savingsID, "transfer_savings.csv", fixtureCSV(t, "transfer_savings.csv"))
	savingsImportID := int64(body["importId"].(float64))

	links := listTransfers(t, ts)
	if len(links) != 1 {
		t.Fatalf("expected 1 auto link, got %+v", links)
	}
	linkID := links[0].ID

	status, _, _ := doJSON(t, ts, "POST", fmt.Sprintf("/api/transfers/%d/unlink", linkID), nil)
	if status != http.StatusOK {
		t.Fatalf("unlink: %d", status)
	}
	before := listActions(t, ts)
	if len(before) != 1 || before[0].Kind != "transfer_unlink" || before[0].ChangeCount != 4 {
		t.Fatalf("expected one transfer_unlink action with 4 changes, got %+v", before)
	}
	if n := actionChangeCount(t, "transfer_rejection"); n != 1 {
		t.Fatalf("expected 1 transfer_rejection change row, got %d", n)
	}

	// Undo the savings import (carries the in side of the rejected pair)
	status, _, _ = doJSON(t, ts, "POST", fmt.Sprintf("/api/imports/%d/undo", savingsImportID), nil)
	if status != http.StatusOK {
		t.Fatalf("undo import: %d", status)
	}

	// The rejection change row touched a deleted line and is cleaned; the
	// action survives with the out-line and link changes.
	if n := actionChangeCount(t, "transfer_rejection"); n != 0 {
		t.Errorf("expected rejection change rows to be cleaned, got %d", n)
	}
	after := listActions(t, ts)
	if len(after) != 1 || after[0].Kind != "transfer_unlink" {
		t.Fatalf("expected the unlink action to survive, got %+v", after)
	}
	if after[0].ChangeCount != before[0].ChangeCount-2 {
		t.Errorf("expected changeCount %d after cleanup, got %d", before[0].ChangeCount-2, after[0].ChangeCount)
	}

	// Detection finds nothing (the savings lines are deleted)
	_, dbody, _ := doJSON(t, ts, "POST", "/api/transfers/detect", nil)
	if dbody["autoLinked"] != float64(0) {
		t.Errorf("detect must find nothing after the undo, got %v", dbody)
	}
}

// T4: rule spanning two imports. The rule_create action references lines of
// both batches; undoing one import removes only that batch's change rows, the
// rule action stays undoable, and undoing it restores the surviving lines while
// the deleted ones stay deleted.
func TestImportUndoKeepsRuleActionSpanningImports(t *testing.T) {
	ts := newTestServer(t)
	accountID := createDBViaAPI(t, ts)
	importCSV(t, ts, accountID, "normal.csv", fixtureCSV(t, "normal.csv"))
	// Import B is the latest, so it is the one the undo endpoint accepts.
	bodyB := importCSV(t, ts, accountID, "transfer_main.csv", fixtureCSV(t, "transfer_main.csv"))
	importB := int64(bodyB["importId"].(float64))

	subID := firstSubcategoryID(t, ts)
	// Lines of each import match
	status, rbody, _ := doJSON(t, ts, "POST", "/api/rules", map[string]any{
		"name": "Transfers and Netto", "pattern": "Transfer|Netto", "direction": "both", "subcategoryId": subID,
	})
	if status != http.StatusCreated {
		t.Fatalf("create rule: %d %v", status, rbody)
	}
	netto := findLine(t, ts, accountID, "Køb i Netto")            // import A (survives the undo)
	transfer := findLine(t, ts, accountID, "Transfer to savings") // import B (deleted by the undo)
	if netto["source"] != "rule" || transfer["source"] != "rule" {
		t.Fatalf("expected both lines rule-categorized, got %v / %v", netto["source"], transfer["source"])
	}
	nettoID := int64(netto["id"].(float64))
	transferID := int64(transfer["id"].(float64))

	before := listActions(t, ts)
	if len(before) != 1 || before[0].Kind != "rule_create" {
		t.Fatalf("expected one rule_create action, got %+v", before)
	}

	// Undo import B (the latest), which carries most of the rule-matched lines
	status, _, _ = doJSON(t, ts, "POST", fmt.Sprintf("/api/imports/%d/undo", importB), nil)
	if status != http.StatusOK {
		t.Fatalf("undo import B: %d", status)
	}
	status, _, _ = doJSON(t, ts, "GET", fmt.Sprintf("/api/lines/%d", transferID), nil)
	if status != http.StatusNotFound {
		t.Fatalf("import-B line must be deleted, got %d", status)
	}

	// The rule action survives; its change rows for the deleted import-B lines
	// were removed (rule rows are never deleted by import undo).
	after := listActions(t, ts)
	if len(after) != 1 || after[0].Kind != "rule_create" {
		t.Fatalf("rule action must survive import undo, got %+v", after)
	}
	if after[0].ChangeCount >= before[0].ChangeCount {
		t.Errorf("expected changeCount to drop after import undo, before=%d after=%d", before[0].ChangeCount, after[0].ChangeCount)
	}
	if after[0].ChangeCount != 2 { // rule + the surviving import-A Netto line
		t.Errorf("expected changeCount 2 (rule + import-A line), got %d", after[0].ChangeCount)
	}

	// Undo the rule action: rule deleted, import-A line restored to its
	// before-snapshot, import-B line stays deleted.
	status, ubody, _ := doJSON(t, ts, "POST", "/api/actions/undo", nil)
	if status != http.StatusOK || ubody["kind"] != "rule_create" {
		t.Fatalf("actions undo: %d %v", status, ubody)
	}
	status, _, _ = doJSON(t, ts, "GET", fmt.Sprintf("/api/lines/%d", transferID), nil)
	if status != http.StatusNotFound {
		t.Errorf("import-B line must stay deleted, got %d", status)
	}
	l := getLineAPI(t, ts, nettoID)
	if l["source"] != "none" {
		t.Errorf("import-A line must be restored to its before-snapshot (none), got %v", l["source"])
	}
	status, _, raw := doJSON(t, ts, "GET", "/api/rules", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/rules: %d", status)
	}
	var rules []map[string]any
	if err := json.Unmarshal(raw, &rules); err != nil || len(rules) != 0 {
		t.Errorf("expected the rule to be deleted by the undo, got %s (err %v)", raw, err)
	}
}

// T5: orphan pruning. An action whose changes all referenced the undone batch
// is removed from action_log entirely, so it does not consume a Ctrl+Z.
func TestImportUndoPrunesOrphanActionLogRows(t *testing.T) {
	ts := newTestServer(t)
	accountID := createDBViaAPI(t, ts)
	body := importCSV(t, ts, accountID, "normal.csv", fixtureCSV(t, "normal.csv"))
	importID := int64(body["importId"].(float64))

	line := findLine(t, ts, accountID, "Køb i Netto")
	lineID := int64(line["id"].(float64))
	subID := firstSubcategoryID(t, ts)
	status, cbody, _ := doJSON(t, ts, "POST", fmt.Sprintf("/api/lines/%d/categorize", lineID), map[string]any{"subcategoryId": subID})
	if status != http.StatusOK {
		t.Fatalf("categorize: %d %v", status, cbody)
	}
	if n := actionLogCount(t); n != 1 {
		t.Fatalf("expected 1 action_log row, got %d", n)
	}
	if n := actionChangeCount(t, "statement_line"); n != 1 {
		t.Fatalf("expected 1 statement_line change row, got %d", n)
	}

	status, _, _ = doJSON(t, ts, "POST", fmt.Sprintf("/api/imports/%d/undo", importID), nil)
	if status != http.StatusOK {
		t.Fatalf("undo import: %d", status)
	}

	if n := actionLogCount(t); n != 0 {
		t.Errorf("expected the orphaned action_log row to be pruned, got %d rows", n)
	}
	if n := actionChangeCount(t, "statement_line"); n != 0 {
		t.Errorf("expected 0 statement_line change rows, got %d", n)
	}
	if actions := listActions(t, ts); len(actions) != 0 {
		t.Errorf("expected no actions listed, got %+v", actions)
	}
	status, _, _ = doJSON(t, ts, "POST", "/api/actions/undo", nil)
	if status != http.StatusConflict {
		t.Errorf("expected 409 nothing_to_undo, got %d", status)
	}
}
