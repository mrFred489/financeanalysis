package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// importNormalCSV creates a DB with one account and imports normal.csv (3 lines).
func importNormalCSV(t *testing.T, ts *httptest.Server) (accountID int64, lineIDs []int64) {
	t.Helper()
	accountID = createDBViaAPI(t, ts)
	content := fixtureCSV(t, "normal.csv")
	buf, ct := multipartBody(t, map[string]string{"accountId": fmt.Sprint(accountID)}, "file", "normal.csv", content)
	status, body, _ := doMultipart(t, ts, "POST", "/api/imports/preview", buf, ct)
	if status != http.StatusOK {
		t.Fatalf("preview: %d %v", status, body)
	}
	token, _ := body["previewToken"].(string)
	buf, ct = multipartBody(t, map[string]string{"accountId": fmt.Sprint(accountID), "previewToken": token}, "file", "normal.csv", content)
	status, body, _ = doMultipart(t, ts, "POST", "/api/imports", buf, ct)
	if status != http.StatusCreated {
		t.Fatalf("commit: %d %v", status, body)
	}
	_, _, raw := doJSON(t, ts, "GET", "/api/lines?pageSize=100", nil)
	var lr struct {
		Lines []struct {
			ID     int64  `json:"id"`
			Source string `json:"source"`
		} `json:"lines"`
	}
	if err := json.Unmarshal(raw, &lr); err != nil {
		t.Fatalf("decode lines: %v", err)
	}
	for _, l := range lr.Lines {
		lineIDs = append(lineIDs, l.ID)
	}
	if len(lineIDs) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lineIDs))
	}
	return accountID, lineIDs
}

// subcategoryIDByName finds a seeded subcategory id via the API.
func subcategoryIDByName(t *testing.T, ts *httptest.Server, name string) int64 {
	t.Helper()
	_, _, raw := doJSON(t, ts, "GET", "/api/subcategories", nil)
	var subs []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &subs); err != nil {
		t.Fatalf("decode subcategories: %v", err)
	}
	for _, s := range subs {
		if s.Name == name {
			return s.ID
		}
	}
	t.Fatalf("subcategory %q not found in %v", name, subs)
	return 0
}

func getLineViaAPI(t *testing.T, ts *httptest.Server, id int64) map[string]any {
	t.Helper()
	status, body, _ := doJSON(t, ts, "GET", fmt.Sprintf("/api/lines/%d", id), nil)
	if status != http.StatusOK {
		t.Fatalf("get line %d: %d %v", id, status, body)
	}
	return body
}

func listLinesViaAPI(t *testing.T, ts *httptest.Server, query string) (int64, []map[string]any) {
	t.Helper()
	status, _, raw := doJSON(t, ts, "GET", "/api/lines?"+query, nil)
	if status != http.StatusOK {
		t.Fatalf("list lines %q: %d", query, status)
	}
	var lr struct {
		Lines []map[string]any `json:"lines"`
		Total int64            `json:"total"`
	}
	if err := json.Unmarshal(raw, &lr); err != nil {
		t.Fatalf("decode lines: %v", err)
	}
	return lr.Total, lr.Lines
}

func TestCategorizeUndoRedoRoundTrip(t *testing.T) {
	ts := newTestServer(t)
	_, lineIDs := importNormalCSV(t, ts)
	subID := subcategoryIDByName(t, ts, "Groceries")
	lineID := lineIDs[0]

	// Categorize -> source=manual, subcategory set
	status, body, _ := doJSON(t, ts, "POST", fmt.Sprintf("/api/lines/%d/categorize", lineID), map[string]any{"subcategoryId": subID})
	if status != http.StatusOK {
		t.Fatalf("categorize: %d %v", status, body)
	}
	if body["source"] != "manual" || body["subcategoryId"] != float64(subID) || body["subcategoryName"] != "Groceries" {
		t.Fatalf("categorize response: %v", body)
	}

	// Action list shows one undoable action
	status, _, raw := doJSON(t, ts, "GET", "/api/actions", nil)
	if status != http.StatusOK {
		t.Fatalf("actions: %d", status)
	}
	var actions []map[string]any
	json.Unmarshal(raw, &actions)
	if len(actions) != 1 || actions[0]["kind"] != "categorize" || actions[0]["undoneAt"] != nil {
		t.Fatalf("actions: %v", actions)
	}

	// Undo -> back to source=none, subcategory null; action marked undone
	status, body, _ = doJSON(t, ts, "POST", "/api/actions/undo", nil)
	if status != http.StatusOK {
		t.Fatalf("undo: %d %v", status, body)
	}
	if body["kind"] != "categorize" {
		t.Fatalf("undo response: %v", body)
	}
	line := getLineViaAPI(t, ts, lineID)
	if line["source"] != "none" || line["subcategoryId"] != nil {
		t.Fatalf("after undo: %v", line)
	}

	// Redo -> manual again
	status, body, _ = doJSON(t, ts, "POST", "/api/actions/redo", nil)
	if status != http.StatusOK {
		t.Fatalf("redo: %d %v", status, body)
	}
	line = getLineViaAPI(t, ts, lineID)
	if line["source"] != "manual" || line["subcategoryId"] != float64(subID) {
		t.Fatalf("after redo: %v", line)
	}

	// Undo again, then redo stack: second redo must 409
	if _, _, _ = doJSON(t, ts, "POST", "/api/actions/undo", nil); true {
	}
	status, _, _ = doJSON(t, ts, "POST", "/api/actions/redo", nil)
	if status != http.StatusOK {
		t.Fatalf("redo2: %d", status)
	}
	status, body, _ = doJSON(t, ts, "POST", "/api/actions/redo", nil)
	if status != http.StatusConflict {
		t.Fatalf("expected 409 on empty redo, got %d %v", status, body)
	}
	if body["error"].(map[string]any)["code"] != "nothing_to_redo" {
		t.Fatalf("error code: %v", body)
	}
}

func TestBulkCategorizeAndUndo(t *testing.T) {
	ts := newTestServer(t)
	_, lineIDs := importNormalCSV(t, ts)
	subID := subcategoryIDByName(t, ts, "Groceries")

	status, body, _ := doJSON(t, ts, "POST", "/api/lines/categorize", map[string]any{
		"lineIds": lineIDs, "subcategoryId": subID,
	})
	if status != http.StatusOK {
		t.Fatalf("bulk categorize: %d %v", status, body)
	}
	lines, ok := body["lines"].([]any)
	if !ok || len(lines) != 3 {
		t.Fatalf("bulk response lines: %v", body)
	}
	for _, l := range lines {
		lm := l.(map[string]any)
		if lm["source"] != "manual" || lm["subcategoryId"] != float64(subID) {
			t.Fatalf("bulk line: %v", lm)
		}
	}

	// Exactly one undoable action for the whole bulk
	_, _, raw := doJSON(t, ts, "GET", "/api/actions", nil)
	var actions []map[string]any
	json.Unmarshal(raw, &actions)
	if len(actions) != 1 || actions[0]["kind"] != "categorize_bulk" || actions[0]["changeCount"] != float64(3) {
		t.Fatalf("actions: %v", actions)
	}

	// One undo restores every row
	status, _, _ = doJSON(t, ts, "POST", "/api/actions/undo", nil)
	if status != http.StatusOK {
		t.Fatalf("undo: %d", status)
	}
	for _, id := range lineIDs {
		line := getLineViaAPI(t, ts, id)
		if line["source"] != "none" || line["subcategoryId"] != nil {
			t.Fatalf("line %d after bulk undo: %v", id, line)
		}
	}
}

func TestSpreadValidationAndUndo(t *testing.T) {
	ts := newTestServer(t)
	_, lineIDs := importNormalCSV(t, ts)
	lineID := lineIDs[0]

	for _, m := range []int64{0, -1, 37} {
		status, body, _ := doJSON(t, ts, "POST", fmt.Sprintf("/api/lines/%d/spread", lineID), map[string]any{"months": m})
		if status != http.StatusBadRequest {
			t.Fatalf("spread %d: expected 400, got %d %v", m, status, body)
		}
	}

	status, body, _ := doJSON(t, ts, "POST", fmt.Sprintf("/api/lines/%d/spread", lineID), map[string]any{"months": 12})
	if status != http.StatusOK || body["spreadMonths"] != float64(12) {
		t.Fatalf("spread 12: %d %v", status, body)
	}

	// Undo restores spread to 1
	status, _, _ = doJSON(t, ts, "POST", "/api/actions/undo", nil)
	if status != http.StatusOK {
		t.Fatalf("undo: %d", status)
	}
	line := getLineViaAPI(t, ts, lineID)
	if line["spreadMonths"] != float64(1) {
		t.Fatalf("after undo: %v", line)
	}
}

func TestResetLine(t *testing.T) {
	ts := newTestServer(t)
	_, lineIDs := importNormalCSV(t, ts)
	subID := subcategoryIDByName(t, ts, "Groceries")
	lineID := lineIDs[0]

	status, _, _ := doJSON(t, ts, "POST", fmt.Sprintf("/api/lines/%d/categorize", lineID), map[string]any{"subcategoryId": subID})
	if status != http.StatusOK {
		t.Fatalf("categorize: %d", status)
	}

	status, body, _ := doJSON(t, ts, "POST", fmt.Sprintf("/api/lines/%d/reset", lineID), nil)
	if status != http.StatusOK || body["source"] != "none" || body["subcategoryId"] != nil {
		t.Fatalf("reset: %d %v", status, body)
	}

	// Undo of the reset restores the manual assignment
	status, _, _ = doJSON(t, ts, "POST", "/api/actions/undo", nil)
	if status != http.StatusOK {
		t.Fatalf("undo: %d", status)
	}
	line := getLineViaAPI(t, ts, lineID)
	if line["source"] != "manual" || line["subcategoryId"] != float64(subID) {
		t.Fatalf("after undo of reset: %v", line)
	}
}

func TestUndoRedoEmpty409(t *testing.T) {
	ts := newTestServer(t)
	importNormalCSV(t, ts)

	status, body, _ := doJSON(t, ts, "POST", "/api/actions/undo", nil)
	if status != http.StatusConflict || body["error"].(map[string]any)["code"] != "nothing_to_undo" {
		t.Fatalf("undo empty: %d %v", status, body)
	}
	status, body, _ = doJSON(t, ts, "POST", "/api/actions/redo", nil)
	if status != http.StatusConflict || body["error"].(map[string]any)["code"] != "nothing_to_redo" {
		t.Fatalf("redo empty: %d %v", status, body)
	}
}

func TestCategorizeValidation(t *testing.T) {
	ts := newTestServer(t)
	_, lineIDs := importNormalCSV(t, ts)
	subID := subcategoryIDByName(t, ts, "Groceries")
	lineID := lineIDs[0]

	// Unknown line -> 404
	status, _, _ := doJSON(t, ts, "POST", "/api/lines/99999/categorize", map[string]any{"subcategoryId": subID})
	if status != http.StatusNotFound {
		t.Fatalf("unknown line: %d", status)
	}
	// Unknown subcategory -> 404
	status, _, _ = doJSON(t, ts, "POST", fmt.Sprintf("/api/lines/%d/categorize", lineID), map[string]any{"subcategoryId": 99999})
	if status != http.StatusNotFound {
		t.Fatalf("unknown subcategory: %d", status)
	}
	// Missing field -> 400
	status, _, _ = doJSON(t, ts, "POST", fmt.Sprintf("/api/lines/%d/categorize", lineID), map[string]any{})
	if status != http.StatusBadRequest {
		t.Fatalf("missing subcategoryId: %d", status)
	}
	// Bulk with empty lineIds -> 400
	status, _, _ = doJSON(t, ts, "POST", "/api/lines/categorize", map[string]any{"lineIds": []int64{}, "subcategoryId": subID})
	if status != http.StatusBadRequest {
		t.Fatalf("empty bulk: %d", status)
	}
	// Invalid line id -> 400
	status, _, _ = doJSON(t, ts, "POST", "/api/lines/abc/categorize", map[string]any{"subcategoryId": subID})
	if status != http.StatusBadRequest {
		t.Fatalf("bad id: %d", status)
	}
}

func TestSourceListFilter(t *testing.T) {
	ts := newTestServer(t)
	_, lineIDs := importNormalCSV(t, ts)
	subID := subcategoryIDByName(t, ts, "Groceries")

	// Categorize one of three lines
	status, _, _ := doJSON(t, ts, "POST", fmt.Sprintf("/api/lines/%d/categorize", lineIDs[0]), map[string]any{"subcategoryId": subID})
	if status != http.StatusOK {
		t.Fatalf("categorize: %d", status)
	}

	// Single value keeps working (D6)
	total, lines := listLinesViaAPI(t, ts, "source=none&pageSize=100")
	if total != 2 {
		t.Fatalf("source=none: total=%d, want 2", total)
	}
	for _, l := range lines {
		if l["source"] != "none" {
			t.Fatalf("source=none returned %v", l["source"])
		}
	}

	// Comma-separated list (categorize page default: none,conflict)
	total, _ = listLinesViaAPI(t, ts, "source=none,conflict&pageSize=100")
	if total != 2 {
		t.Fatalf("source=none,conflict: total=%d, want 2", total)
	}
	total, _ = listLinesViaAPI(t, ts, "source=manual&pageSize=100")
	if total != 1 {
		t.Fatalf("source=manual: total=%d, want 1", total)
	}
	total, _ = listLinesViaAPI(t, ts, "source=none,manual&pageSize=100")
	if total != 3 {
		t.Fatalf("source=none,manual: total=%d, want 3", total)
	}

	// Invalid source value -> 400
	status, _, _ = doJSON(t, ts, "GET", "/api/lines?source=bogus", nil)
	if status != http.StatusBadRequest {
		t.Fatalf("source=bogus: %d", status)
	}
}
