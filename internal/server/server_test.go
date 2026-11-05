package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrFred489/financeanalysis/internal/db"
)

// newTestServer creates a Server with a temp data dir and returns an httptest server for it.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	db.Close() // reset any global handle from a previous server in the same test
	dataDir := t.TempDir()
	srv, err := NewServer("127.0.0.1:0", dataDir)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)
	// Reset the global DB handle so each test starts clean
	t.Cleanup(func() { db.Close() })
	return ts
}

func doJSON(t *testing.T, ts *httptest.Server, method, path string, body any) (int, map[string]any, []byte) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, ts.URL+path, reader)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	raw := new(bytes.Buffer)
	raw.ReadFrom(resp.Body)
	var parsed map[string]any
	json.Unmarshal(raw.Bytes(), &parsed)
	return resp.StatusCode, parsed, raw.Bytes()
}

func multipartBody(t *testing.T, fields map[string]string, fileField, fileName string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	buf := new(bytes.Buffer)
	w := multipart.NewWriter(buf)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatalf("WriteField: %v", err)
		}
	}
	if fileField != "" {
		fw, err := w.CreateFormFile(fileField, fileName)
		if err != nil {
			t.Fatalf("CreateFormFile: %v", err)
		}
		if _, err := fw.Write(content); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return buf, w.FormDataContentType()
}

func doMultipart(t *testing.T, ts *httptest.Server, method, path string, buf *bytes.Buffer, contentType string) (int, map[string]any, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+path, buf)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	raw := new(bytes.Buffer)
	raw.ReadFrom(resp.Body)
	var parsed map[string]any
	json.Unmarshal(raw.Bytes(), &parsed)
	return resp.StatusCode, parsed, raw.Bytes()
}

func fixtureCSV(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "csv", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

// createDBViaAPI runs the welcome-wizard creation flow and returns the account id.
func createDBViaAPI(t *testing.T, ts *httptest.Server) int64 {
	t.Helper()
	status, body, _ := doJSON(t, ts, "POST", "/api/database", map[string]any{
		"accounts": []map[string]any{{"name": "Main account", "account_number": "9999 0000000001"}},
		"subcategories": []map[string]any{
			{"category_key": "variable", "name": "Groceries", "sort_order": 0},
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("create database: status %d body %v", status, body)
	}
	status, _, _ = doJSON(t, ts, "GET", "/api/accounts", nil)
	if status != http.StatusOK {
		t.Fatalf("list accounts: status %d", status)
	}
	_, _, raw := doJSON(t, ts, "GET", "/api/accounts", nil)
	var accs []map[string]any
	if err := json.Unmarshal(raw, &accs); err != nil {
		t.Fatalf("decode accounts: %v (%s)", err, raw)
	}
	if len(accs) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accs))
	}
	return int64(accs[0]["id"].(float64))
}

func TestStatusMissingThenReady(t *testing.T) {
	ts := newTestServer(t)

	status, body, _ := doJSON(t, ts, "GET", "/api/status", nil)
	if status != http.StatusOK {
		t.Fatalf("status: %d", status)
	}
	if body["database"] != "missing" {
		t.Errorf("expected database=missing, got %v", body["database"])
	}

	accountID := createDBViaAPI(t, ts)
	if accountID == 0 {
		t.Fatalf("expected nonzero account id")
	}

	_, body, _ = doJSON(t, ts, "GET", "/api/status", nil)
	if body["database"] != "ready" {
		t.Errorf("expected database=ready, got %v", body["database"])
	}
	if body["accountsConfigured"] != true {
		t.Errorf("expected accountsConfigured=true, got %v", body["accountsConfigured"])
	}
	if body["schemaVersion"] != float64(db.CurrentSchemaVersion) {
		t.Errorf("expected schemaVersion=%d, got %v", db.CurrentSchemaVersion, body["schemaVersion"])
	}

	// Second create must conflict
	status, _, _ = doJSON(t, ts, "POST", "/api/database", nil)
	if status != http.StatusConflict {
		t.Errorf("expected 409 on second create, got %d", status)
	}
}

func TestAccountsCRUD(t *testing.T) {
	ts := newTestServer(t)
	accountID := createDBViaAPI(t, ts)

	// Create another account
	status, body, _ := doJSON(t, ts, "POST", "/api/accounts", map[string]any{"name": "Savings"})
	if status != http.StatusCreated {
		t.Fatalf("create account: %d %v", status, body)
	}
	secondID := int64(body["id"].(float64))

	// Rename + archive
	status, body, _ = doJSON(t, ts, "PUT", fmt.Sprintf("/api/accounts/%d", secondID), map[string]any{"name": "Savings account", "archived": true})
	if status != http.StatusOK || body["name"] != "Savings account" || body["archived"] != float64(1) {
		t.Errorf("update account: %d %v", status, body)
	}

	// Delete with no lines works
	status, _, _ = doJSON(t, ts, "DELETE", fmt.Sprintf("/api/accounts/%d", secondID), nil)
	if status != http.StatusOK {
		t.Errorf("delete account: %d", status)
	}

	// Delete the account that will have lines must fail (SETUP-6) - tested after import
	_ = accountID
}

func TestSubcategoriesCRUDAndMerge(t *testing.T) {
	ts := newTestServer(t)
	createDBViaAPI(t, ts)

	// Categories come with seeded (wizard-replaced) subcategories
	status, _, raw := doJSON(t, ts, "GET", "/api/categories", nil)
	if status != http.StatusOK {
		t.Fatalf("categories: %d", status)
	}
	var cats []map[string]any
	json.Unmarshal(raw, &cats)
	if len(cats) != 4 {
		t.Fatalf("expected 4 categories, got %d", len(cats))
	}

	// Create a subcategory; color auto-generated (CAT-5)
	status, body, _ := doJSON(t, ts, "POST", "/api/subcategories", map[string]any{"category_id": 3, "name": "Coffee"})
	if status != http.StatusCreated {
		t.Fatalf("create subcategory: %d %v", status, body)
	}
	subID := int64(body["id"].(float64))
	color, _ := body["color"].(string)
	if color == "" || !strings.HasPrefix(color, "#") {
		t.Errorf("expected auto-generated hex color, got %q", color)
	}

	// Update: move to another category + rename (CAT-3)
	status, body, _ = doJSON(t, ts, "PUT", fmt.Sprintf("/api/subcategories/%d", subID), map[string]any{"name": "Coffee & tea", "category_id": 2})
	if status != http.StatusOK || body["name"] != "Coffee & tea" || body["category_id"] != float64(2) {
		t.Errorf("update subcategory: %d %v", status, body)
	}

	// Delete without mergeIntoId -> 400
	status, _, _ = doJSON(t, ts, "DELETE", fmt.Sprintf("/api/subcategories/%d", subID), nil)
	if status != http.StatusBadRequest {
		t.Errorf("expected 400 without mergeIntoId, got %d", status)
	}

	// Delete with mergeIntoId -> 200 (CAT-4)
	status, _, _ = doJSON(t, ts, "DELETE", fmt.Sprintf("/api/subcategories/%d?mergeIntoId=1", subID), nil)
	if status != http.StatusOK {
		t.Errorf("expected 200 with mergeIntoId, got %d", status)
	}
}

func TestImportFlowPreviewCommitUndo(t *testing.T) {
	ts := newTestServer(t)
	accountID := createDBViaAPI(t, ts)
	content := fixtureCSV(t, "normal.csv")

	// Preview
	buf, ct := multipartBody(t, map[string]string{"accountId": fmt.Sprint(accountID)}, "file", "normal.csv", content)
	status, body, _ := doMultipart(t, ts, "POST", "/api/imports/preview", buf, ct)
	if status != http.StatusOK {
		t.Fatalf("preview: %d %v", status, body)
	}
	if body["newCount"] != float64(3) || body["duplicateCount"] != float64(0) {
		t.Errorf("preview counts: %v", body)
	}
	token, _ := body["previewToken"].(string)
	if token == "" {
		t.Fatalf("no previewToken in %v", body)
	}

	// Commit with wrong token -> 400
	buf, ct = multipartBody(t, map[string]string{"accountId": fmt.Sprint(accountID), "previewToken": "bogus"}, "file", "normal.csv", content)
	status, _, _ = doMultipart(t, ts, "POST", "/api/imports", buf, ct)
	if status != http.StatusBadRequest {
		t.Errorf("expected 400 for bad token, got %d", status)
	}

	// Commit
	buf, ct = multipartBody(t, map[string]string{"accountId": fmt.Sprint(accountID), "previewToken": token}, "file", "normal.csv", content)
	status, body, _ = doMultipart(t, ts, "POST", "/api/imports", buf, ct)
	if status != http.StatusCreated {
		t.Fatalf("commit: %d %v", status, body)
	}
	if body["linesImported"] != float64(3) {
		t.Errorf("expected 3 imported, got %v", body)
	}
	importID := int64(body["importId"].(float64))

	// Lines visible with totals
	status, _, raw := doJSON(t, ts, "GET", "/api/lines", nil)
	if status != http.StatusOK {
		t.Fatalf("lines: %d", status)
	}
	var lr struct {
		Lines  []map[string]any `json:"lines"`
		Total  int64            `json:"total"`
		Totals struct {
			Count  int64 `json:"count"`
			SumIn  int64 `json:"sumIn"`
			SumOut int64 `json:"sumOut"`
			Net    int64 `json:"net"`
		} `json:"totals"`
	}
	json.Unmarshal(raw, &lr)
	if lr.Total != 3 || len(lr.Lines) != 3 {
		t.Fatalf("expected 3 lines, got total=%d lines=%d", lr.Total, len(lr.Lines))
	}
	// 25000.00 in, -1607.97 out, net 23392.03
	if lr.Totals.SumIn != 2500000 || lr.Totals.SumOut != -160797 || lr.Totals.Net != 2339203 {
		t.Errorf("unexpected totals: %+v", lr.Totals)
	}
	if lr.Lines[0]["bookingDate"] != "2024-12-31" {
		t.Errorf("expected newest first, got %v", lr.Lines[0]["bookingDate"])
	}
	if lr.Lines[0]["source"] != "none" {
		t.Errorf("expected source none, got %v", lr.Lines[0]["source"])
	}
	// v2 format: no balance (null), bank_ref stored from column 10
	if lr.Lines[0]["balanceMinor"] != nil {
		t.Errorf("expected null balanceMinor, got %v", lr.Lines[0]["balanceMinor"])
	}
	if lr.Lines[0]["bankRef"] != "0000000001" {
		t.Errorf("expected bankRef 0000000001, got %v", lr.Lines[0]["bankRef"])
	}

	// Filter by message (server-side)
	_, _, raw = doJSON(t, ts, "GET", "/api/lines?message=Netto", nil)
	json.Unmarshal(raw, &lr)
	if lr.Total != 1 {
		t.Errorf("expected 1 line matching 'Netto', got %d", lr.Total)
	}

	// Filter by direction=in
	_, _, raw = doJSON(t, ts, "GET", "/api/lines?direction=in", nil)
	json.Unmarshal(raw, &lr)
	if lr.Total != 1 || lr.Totals.SumIn != 2500000 {
		t.Errorf("direction=in: total=%d totals=%+v", lr.Total, lr.Totals)
	}

	// Sorting by amount asc
	_, _, raw = doJSON(t, ts, "GET", "/api/lines?sort=amount&order=asc", nil)
	json.Unmarshal(raw, &lr)
	if len(lr.Lines) == 3 && lr.Lines[0]["amountMinor"] != float64(-135797) {
		t.Errorf("expected most negative first, got %v", lr.Lines[0]["amountMinor"])
	}

	// Pagination
	_, _, raw = doJSON(t, ts, "GET", "/api/lines?page=2&pageSize=2", nil)
	json.Unmarshal(raw, &lr)
	if len(lr.Lines) != 1 || lr.Total != 3 {
		t.Errorf("page 2 size 2: lines=%d total=%d", len(lr.Lines), lr.Total)
	}

	// Re-import same file -> all duplicates
	buf, ct = multipartBody(t, map[string]string{"accountId": fmt.Sprint(accountID)}, "file", "normal.csv", content)
	_, body, _ = doMultipart(t, ts, "POST", "/api/imports/preview", buf, ct)
	if body["duplicateCount"] != float64(3) || body["newCount"] != float64(0) {
		t.Errorf("re-preview: expected 3 dups, got %v", body)
	}
	hasDupFile := false
	for _, w := range body["warnings"].([]any) {
		if w.(map[string]any)["type"] == "duplicate_file" {
			hasDupFile = true
		}
	}
	if !hasDupFile {
		t.Errorf("expected duplicate_file warning, got %v", body["warnings"])
	}

	// Import history
	status, _, raw = doJSON(t, ts, "GET", "/api/imports", nil)
	if status != http.StatusOK {
		t.Fatalf("imports: %d", status)
	}
	var batches []map[string]any
	json.Unmarshal(raw, &batches)
	if len(batches) != 1 || batches[0]["id"] != float64(importID) {
		t.Fatalf("expected 1 batch with id %d, got %v", importID, batches)
	}
	if batches[0]["account_name"] != "Main account" || batches[0]["date_from"] != "2024-12-29" {
		t.Errorf("unexpected batch: %v", batches[0])
	}
	// An active batch must serialize undone_at as the invalid null-shape
	// ({"String":"","Valid":false}), not as a set timestamp.
	if ua, ok := batches[0]["undone_at"].(map[string]any); !ok || ua["Valid"] != false {
		t.Errorf("expected undone_at Valid=false for an active batch, got %v", batches[0]["undone_at"])
	}

	// Undo a non-latest id is fine here (only one), then lines are gone
	status, _, _ = doJSON(t, ts, "POST", fmt.Sprintf("/api/imports/%d/undo", importID), nil)
	if status != http.StatusOK {
		t.Fatalf("undo: %d", status)
	}
	_, _, raw = doJSON(t, ts, "GET", "/api/lines", nil)
	json.Unmarshal(raw, &lr)
	if lr.Total != 0 {
		t.Errorf("expected 0 lines after undo, got %d", lr.Total)
	}

	// Batch marked undone: undone_at must be Valid=true with a timestamp
	_, _, raw = doJSON(t, ts, "GET", "/api/imports", nil)
	json.Unmarshal(raw, &batches)
	ua, ok := batches[0]["undone_at"].(map[string]any)
	if !ok || ua["Valid"] != true || ua["String"] == "" {
		t.Errorf("expected undone_at Valid=true with a timestamp after undo, got %v", batches[0]["undone_at"])
	}

	// Undo again -> 409 nothing to undo
	status, _, _ = doJSON(t, ts, "POST", fmt.Sprintf("/api/imports/%d/undo", importID), nil)
	if status != http.StatusConflict {
		t.Errorf("expected 409 on second undo, got %d", status)
	}
}

func TestAccountDeleteWithLines(t *testing.T) {
	ts := newTestServer(t)
	accountID := createDBViaAPI(t, ts)
	content := fixtureCSV(t, "normal.csv")

	buf, ct := multipartBody(t, map[string]string{"accountId": fmt.Sprint(accountID)}, "file", "normal.csv", content)
	_, body, _ := doMultipart(t, ts, "POST", "/api/imports/preview", buf, ct)
	token := body["previewToken"].(string)
	buf, ct = multipartBody(t, map[string]string{"accountId": fmt.Sprint(accountID), "previewToken": token}, "file", "normal.csv", content)
	doMultipart(t, ts, "POST", "/api/imports", buf, ct)

	status, _, _ := doJSON(t, ts, "DELETE", fmt.Sprintf("/api/accounts/%d", accountID), nil)
	if status != http.StatusConflict {
		t.Errorf("expected 409 deleting account with lines, got %d", status)
	}
}

func TestSettings(t *testing.T) {
	ts := newTestServer(t)
	createDBViaAPI(t, ts)

	status, _, _ := doJSON(t, ts, "PUT", "/api/settings", map[string]any{"locale": "da-DK"})
	if status != http.StatusOK {
		t.Fatalf("put settings: %d", status)
	}
	_, _, raw := doJSON(t, ts, "GET", "/api/settings", nil)
	var res struct {
		Settings map[string]string `json:"settings"`
	}
	json.Unmarshal(raw, &res)
	if res.Settings["locale"] != "da-DK" {
		t.Errorf("expected locale=da-DK, got %v", res.Settings)
	}
}

func TestDatabaseDownloadUploadReplaceBackups(t *testing.T) {
	ts := newTestServer(t)
	createDBViaAPI(t, ts)

	// Download
	resp, err := http.Get(ts.URL + "/api/database/download")
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	raw := new(bytes.Buffer)
	raw.ReadFrom(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || raw.Len() == 0 {
		t.Fatalf("download: status %d len %d", resp.StatusCode, raw.Len())
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Errorf("expected attachment Content-Disposition, got %q", cd)
	}
	downloaded := raw.Bytes()

	// Uploading the downloaded file to the same (existing) DB -> 409 on upload endpoint
	buf, ct := multipartBody(t, nil, "file", "finance.sqlite", downloaded)
	status, _, _ := doMultipart(t, ts, "POST", "/api/database/upload", buf, ct)
	if status != http.StatusConflict {
		t.Errorf("expected 409 uploading over existing DB, got %d", status)
	}

	// Replace with the downloaded file -> 200
	buf, ct = multipartBody(t, nil, "file", "finance.sqlite", downloaded)
	status, _, _ = doMultipart(t, ts, "POST", "/api/database/replace", buf, ct)
	if status != http.StatusOK {
		t.Errorf("expected 200 on replace, got %d", status)
	}

	// Backups list contains the pre-replace backup
	_, _, raw2 := doJSON(t, ts, "GET", "/api/backups", nil)
	var backups []map[string]any
	json.Unmarshal(raw2, &backups)
	if len(backups) == 0 {
		t.Fatalf("expected at least one backup, got %s", raw2)
	}
	backupName, _ := backups[0]["name"].(string)
	if !strings.Contains(backupName, "pre-replace") && !strings.Contains(backupName, "pre-migrate") {
		t.Logf("backup name: %s", backupName)
	}

	// Restore it
	status, _, _ = doJSON(t, ts, "POST", "/api/backups/"+backupName+"/restore", nil)
	if status != http.StatusOK {
		t.Errorf("expected 200 on restore, got %d", status)
	}

	// Invalid restore name -> 400
	status, _, _ = doJSON(t, ts, "POST", "/api/backups/..%2F..%2Fetc/restore", nil)
	if status != http.StatusBadRequest && status != http.StatusNotFound {
		t.Errorf("expected 400/404 on bad backup name, got %d", status)
	}

	// Upload a non-database file to a fresh server -> 422
	ts2 := newTestServer(t)
	buf, ct = multipartBody(t, nil, "file", "notadb.txt", []byte("hello"))
	status, body, _ := doMultipart(t, ts2, "POST", "/api/database/upload", buf, ct)
	if status != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 for non-database upload, got %d %v", status, body)
	}

	// Upload the downloaded DB to the fresh server -> 201, then status ready
	buf, ct = multipartBody(t, nil, "file", "finance.sqlite", downloaded)
	status, _, _ = doMultipart(t, ts2, "POST", "/api/database/upload", buf, ct)
	if status != http.StatusCreated {
		t.Fatalf("expected 201 uploading valid DB, got %d", status)
	}
	_, body, _ = doJSON(t, ts2, "GET", "/api/status", nil)
	if body["database"] != "ready" {
		t.Errorf("expected ready after upload, got %v", body)
	}
}

func TestSchemaTooNewUploadRefused(t *testing.T) {
	ts := newTestServer(t)
	createDBViaAPI(t, ts)

	// Make a DB with a newer schema version
	tmp := t.TempDir()
	newDBPath := filepath.Join(tmp, "newer.sqlite")
	d, err := sql.Open("sqlite", newDBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Exec("CREATE TABLE category (id INTEGER PRIMARY KEY, key TEXT UNIQUE NOT NULL, name TEXT NOT NULL, color TEXT NOT NULL) STRICT; INSERT INTO category VALUES (1,'income','Income','#fff'); CREATE TABLE goose_db_version (id INTEGER PRIMARY KEY AUTOINCREMENT, version_id INTEGER NOT NULL UNIQUE, is_applied BOOLEAN NOT NULL DEFAULT 1, tstamp TEXT NOT NULL DEFAULT (datetime('now'))); INSERT INTO goose_db_version (version_id, is_applied) VALUES (999, 1);"); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(newDBPath)
	if err != nil {
		t.Fatal(err)
	}

	ts2 := newTestServer(t)
	buf, ct := multipartBody(t, nil, "file", "newer.sqlite", content)
	status, body, _ := doMultipart(t, ts2, "POST", "/api/database/upload", buf, ct)
	if status != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 for newer schema, got %d", status)
	}
	if body["error"].(map[string]any)["code"] != "schema_too_new" {
		t.Errorf("expected schema_too_new, got %v", body)
	}
}

func TestAPINotFoundAndDBRequired(t *testing.T) {
	ts := newTestServer(t)

	// Unknown API route -> JSON 404
	status, body, _ := doJSON(t, ts, "GET", "/api/nonexistent", nil)
	if status != http.StatusNotFound || body["error"] == nil {
		t.Errorf("expected JSON 404, got %d %v", status, body)
	}

	// DB-required endpoint without DB -> 503
	status, body, _ = doJSON(t, ts, "GET", "/api/accounts", nil)
	if status != http.StatusServiceUnavailable || body["error"].(map[string]any)["code"] != "database_missing" {
		t.Errorf("expected 503 database_missing, got %d %v", status, body)
	}
}
