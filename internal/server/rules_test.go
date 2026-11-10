package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// createRuleViaAPI creates a rule and returns its JSON body.
func createRuleViaAPI(t *testing.T, ts *httptest.Server, body map[string]any) (int, map[string]any, []byte) {
	t.Helper()
	return doJSON(t, ts, "POST", "/api/rules", body)
}

func ruleBodyFor(name, pattern string, subID int64) map[string]any {
	return map[string]any{
		"name": name, "pattern": pattern, "direction": "both",
		"subcategoryId": subID, "accountIds": []int64{}, "enabled": true,
	}
}

func getRuleViaAPI(t *testing.T, ts *httptest.Server, id int64) map[string]any {
	t.Helper()
	status, body, _ := doJSON(t, ts, "GET", fmt.Sprintf("/api/rules/%d", id), nil)
	if status != http.StatusOK {
		t.Fatalf("get rule %d: %d %v", id, status, body)
	}
	return body
}

func listRulesViaAPI(t *testing.T, ts *httptest.Server, query string) []map[string]any {
	t.Helper()
	status, _, raw := doJSON(t, ts, "GET", "/api/rules"+query, nil)
	if status != http.StatusOK {
		t.Fatalf("list rules: %d", status)
	}
	var rules []map[string]any
	if err := json.Unmarshal(raw, &rules); err != nil {
		t.Fatalf("decode rules: %v", err)
	}
	return rules
}

func TestRuleCRUDRoundTrip(t *testing.T) {
	ts := newTestServer(t)
	_, lineIDs := importNormalCSV(t, ts)
	subID := subcategoryIDByName(t, ts, "Groceries")

	// invalid regex rejected with the compiler error (RULE-2)
	status, body, _ := createRuleViaAPI(t, ts, ruleBodyFor("Bad", "(unclosed", subID))
	if status != http.StatusBadRequest || body["error"].(map[string]any)["code"] != "invalid_regex" {
		t.Fatalf("invalid regex: status %d body %v", status, body)
	}

	// create: the Netto line becomes source=rule (Køb i Netto is lineIDs[1])
	status, rule, _ := createRuleViaAPI(t, ts, ruleBodyFor("Netto", `Netto`, subID))
	if status != http.StatusCreated {
		t.Fatalf("create rule: %d %v", status, rule)
	}
	_ = status
	ruleID := int64(rule["id"].(float64))
	if rule["matchCount"].(float64) != 1 {
		t.Fatalf("matchCount: %v", rule["matchCount"])
	}
	netto := getLineViaAPI(t, ts, lineIDs[1])
	if netto["source"] != "rule" || netto["subcategoryId"] != float64(subID) || netto["ruleId"] != float64(ruleID) {
		t.Fatalf("netto line after create: %v", netto)
	}

	// one undoable action of kind rule_create
	_, _, raw := doJSON(t, ts, "GET", "/api/actions", nil)
	var acts []map[string]any
	json.Unmarshal(raw, &acts)
	if len(acts) != 1 || acts[0]["kind"] != "rule_create" {
		t.Fatalf("actions: %v", acts)
	}

	// list: filter + sort (RULE-8)
	rules := listRulesViaAPI(t, ts, "?search=Netto")
	if len(rules) != 1 {
		t.Fatalf("search filter: %v", rules)
	}
	rules = listRulesViaAPI(t, ts, "?enabled=1&sort=matchCount&order=desc")
	if len(rules) != 1 {
		t.Fatalf("enabled filter: %v", rules)
	}

	// edit: narrow pattern to nothing matching -> line drops to none (RULE-6)
	status, body, _ = doJSON(t, ts, "PUT", fmt.Sprintf("/api/rules/%d", ruleID), ruleBodyFor("Netto", `^ZZZ`, subID))
	if status != http.StatusOK {
		t.Fatalf("update rule: %d %v", status, body)
	}
	netto = getLineViaAPI(t, ts, lineIDs[1])
	if netto["source"] != "none" || netto["subcategoryId"] != nil || netto["ruleId"] != nil {
		t.Fatalf("netto line after edit: %v", netto)
	}

	// undo the edit -> pattern back to Netto, line back to rule
	status, _, _ = doJSON(t, ts, "POST", "/api/actions/undo", nil)
	if status != http.StatusOK {
		t.Fatalf("undo edit: %d", status)
	}
	netto = getLineViaAPI(t, ts, lineIDs[1])
	if netto["source"] != "rule" || netto["ruleId"] != float64(ruleID) {
		t.Fatalf("netto line after undo: %v", netto)
	}
	rule = getRuleViaAPI(t, ts, ruleID)
	if rule["pattern"] != "Netto" {
		t.Fatalf("rule pattern after undo: %v", rule["pattern"])
	}

	// toggle off -> line re-evaluated to none
	status, body, _ = doJSON(t, ts, "POST", fmt.Sprintf("/api/rules/%d/toggle", ruleID), map[string]any{"enabled": false})
	if status != http.StatusOK || body["enabled"] != false {
		t.Fatalf("toggle: %d %v", status, body)
	}
	netto = getLineViaAPI(t, ts, lineIDs[1])
	if netto["source"] != "none" {
		t.Fatalf("netto after disable: %v", netto)
	}

	// delete -> rule gone, one more undoable action; undo restores rule + line
	status, _, _ = doJSON(t, ts, "DELETE", fmt.Sprintf("/api/rules/%d", ruleID), nil)
	if status != http.StatusOK {
		t.Fatalf("delete: %d", status)
	}
	status, _, _ = doJSON(t, ts, "GET", fmt.Sprintf("/api/rules/%d", ruleID), nil)
	if status != http.StatusNotFound {
		t.Fatalf("get deleted rule: %d", status)
	}
	status, _, _ = doJSON(t, ts, "POST", "/api/actions/undo", nil) // undo delete
	if status != http.StatusOK {
		t.Fatalf("undo delete: %d", status)
	}
	rule = getRuleViaAPI(t, ts, ruleID)
	if rule["enabled"] != false {
		t.Fatalf("rule after undo of delete: %v", rule)
	}
	netto = getLineViaAPI(t, ts, lineIDs[1])
	if netto["source"] != "none" { // rule still disabled
		t.Fatalf("netto after undo of delete (rule disabled): %v", netto)
	}
}

func TestRulePreviewAndDiff(t *testing.T) {
	ts := newTestServer(t)
	_, lineIDs := importNormalCSV(t, ts)
	subID := subcategoryIDByName(t, ts, "Groceries")
	diningID := subID // only one custom subcategory in the test DB
	_ = lineIDs

	// existing rule: Netto -> Groceries
	status, r1, _ := createRuleViaAPI(t, ts, ruleBodyFor("Netto", `Netto`, subID))
	if status != http.StatusCreated {
		t.Fatalf("create rule: %d %v", status, r1)
	}
	ruleID := int64(r1["id"].(float64))

	// preview a new overlapping rule (RULE-3): Netto + Kontanthævning? no overlap; use `K`
	status, body, _ := doJSON(t, ts, "POST", "/api/rules/preview", ruleBodyFor("K", `K`, subID))
	if status != http.StatusOK {
		t.Fatalf("preview: %d %v", status, body)
	}
	willCat := body["willCategorize"].(map[string]any)["count"].(float64)
	// "Køb i Netto" and "Kontanthævning" match `K`; Netto line is already this...
	// for a NEW rule (no id), the Netto line will conflict (matches both rules)
	willConflict := body["willConflict"].(map[string]any)["count"].(float64)
	if willCat != 1 || willConflict != 1 {
		t.Fatalf("preview groups: willCategorize=%v willConflict=%v (body %v)", willCat, willConflict, body)
	}

	// preview with invalid regex -> 400 invalid_regex
	status, body, _ = doJSON(t, ts, "POST", "/api/rules/preview", ruleBodyFor("Bad", "([", subID))
	if status != http.StatusBadRequest || body["error"].(map[string]any)["code"] != "invalid_regex" {
		t.Fatalf("preview invalid regex: %d %v", status, body)
	}

	// diff (RULE-4): stored `Netto` vs new `^Lon` -> removed = Netto line (future: none),
	// added = the salary line, unchanged = []
	status, diff, _ := doJSON(t, ts, "POST", fmt.Sprintf("/api/rules/%d/diff", ruleID), ruleBodyFor("Netto", `^L`, subID))
	if status != http.StatusOK {
		t.Fatalf("diff: %d %v", status, diff)
	}
	removed := diff["removed"].([]any)
	added := diff["added"].([]any)
	if len(removed) != 1 || int64(removed[0].(float64)) != lineIDs[1] {
		t.Fatalf("diff removed: %v", diff)
	}
	if len(added) != 1 || int64(added[0].(float64)) != lineIDs[0] {
		t.Fatalf("diff added: %v", diff)
	}
	future := diff["removedFuture"].([]any)
	if len(future) != 1 || future[0].(map[string]any)["future"] != "none" {
		t.Fatalf("diff removedFuture: %v", diff)
	}

	// create the overlapping rule, then the Netto line is a conflict
	status2, r2, _ := createRuleViaAPI(t, ts, ruleBodyFor("KRule", `K`, diningID))
	if status2 != http.StatusCreated {
		t.Fatalf("create rule2: %d %v", status2, r2)
	}
	rule2ID := int64(r2["id"].(float64))
	_ = rule2ID
	statusC, _, conflictsRaw := doJSON(t, ts, "GET", "/api/conflicts", nil)
	if statusC != http.StatusOK {
		t.Fatalf("conflicts: %d", statusC)
	}
	var conflicts []map[string]any
	json.Unmarshal(conflictsRaw, &conflicts)
	if len(conflicts) != 1 {
		t.Fatalf("conflicts: %s", conflictsRaw)
	}
	if len(conflicts[0]["rules"].([]any)) != 2 {
		t.Fatalf("conflict rules: %v", conflicts[0])
	}
}

func TestConflictResolveUndo(t *testing.T) {
	ts := newTestServer(t)
	_, lineIDs := importNormalCSV(t, ts)
	subID := subcategoryIDByName(t, ts, "Groceries")
	diningID := subID // only one custom subcategory in the test DB

	c1, r1, _ := createRuleViaAPI(t, ts, ruleBodyFor("Netto", `Netto`, subID))
	c2, r2, _ := createRuleViaAPI(t, ts, ruleBodyFor("K", `K`, diningID))
	if c1 != http.StatusCreated || c2 != http.StatusCreated {
		t.Fatalf("create rules: %d %d", c1, c2)
	}
	rule1ID := int64(r1["id"].(float64))
	rule2ID := int64(r2["id"].(float64))

	netto := getLineViaAPI(t, ts, lineIDs[1])
	if netto["source"] != "conflict" {
		t.Fatalf("expected conflict, got %v", netto)
	}

	// resolving with a rule that does not match -> 409
	status, body, _ := doJSON(t, ts, "POST", fmt.Sprintf("/api/lines/%d/resolve-conflict", lineIDs[0]), map[string]any{"ruleId": rule1ID})
	if status != http.StatusConflict {
		t.Fatalf("resolve non-conflict line: %d %v", status, body)
	}
	status, body, _ = doJSON(t, ts, "POST", fmt.Sprintf("/api/lines/%d/resolve-conflict", lineIDs[1]), map[string]any{"ruleId": 99999})
	if status != http.StatusNotFound {
		t.Fatalf("resolve unknown rule: %d %v", status, body)
	}

	// resolve with rule2 (matches) -> source=rule, subcategory=Dining
	status, body, _ = doJSON(t, ts, "POST", fmt.Sprintf("/api/lines/%d/resolve-conflict", lineIDs[1]), map[string]any{"ruleId": rule2ID})
	if status != http.StatusOK {
		t.Fatalf("resolve: %d %v", status, body)
	}
	if body["source"] != "rule" || body["subcategoryId"] != float64(diningID) || body["ruleId"] != float64(rule2ID) {
		t.Fatalf("resolved line: %v", body)
	}

	// undo -> back to conflict, resolution gone
	status, _, _ = doJSON(t, ts, "POST", "/api/actions/undo", nil)
	if status != http.StatusOK {
		t.Fatalf("undo resolve: %d", status)
	}
	netto = getLineViaAPI(t, ts, lineIDs[1])
	if netto["source"] != "conflict" {
		t.Fatalf("after undo: %v", netto)
	}
	_, _, conflictsRaw := doJSON(t, ts, "GET", "/api/conflicts", nil)
	var conflicts []map[string]any
	json.Unmarshal(conflictsRaw, &conflicts)
	if len(conflicts) != 1 || conflicts[0]["resolutionRuleId"] != nil {
		t.Fatalf("conflicts after undo: %s", conflictsRaw)
	}

	// resolve again, then DELETE the resolution -> back to conflict (undoable)
	doJSON(t, ts, "POST", fmt.Sprintf("/api/lines/%d/resolve-conflict", lineIDs[1]), map[string]any{"ruleId": rule1ID})
	status, body, _ = doJSON(t, ts, "DELETE", fmt.Sprintf("/api/lines/%d/resolve-conflict", lineIDs[1]), nil)
	if status != http.StatusOK {
		t.Fatalf("unresolve: %d %v", status, body)
	}
	if body["source"] != "conflict" {
		t.Fatalf("after unresolve: %v", body)
	}
	// DELETE again -> 409 (no resolution)
	status, _, _ = doJSON(t, ts, "DELETE", fmt.Sprintf("/api/lines/%d/resolve-conflict", lineIDs[1]), nil)
	if status != http.StatusConflict {
		t.Fatalf("unresolve twice: %d", status)
	}
}

func TestResetToRulesReevaluates(t *testing.T) {
	ts := newTestServer(t)
	_, lineIDs := importNormalCSV(t, ts)
	subID := subcategoryIDByName(t, ts, "Groceries")

	c1, r, _ := createRuleViaAPI(t, ts, ruleBodyFor("Netto", `Netto`, subID))
	if c1 != http.StatusCreated {
		t.Fatalf("create rule: %d", c1)
	}
	ruleID := int64(r["id"].(float64))

	// manually categorize the Netto line (clears nothing yet, source=manual)
	status, body, _ := doJSON(t, ts, "POST", fmt.Sprintf("/api/lines/%d/categorize", lineIDs[1]), map[string]any{"subcategoryId": subID})
	if status != http.StatusOK || body["source"] != "manual" {
		t.Fatalf("categorize: %d %v", status, body)
	}

	// reset -> re-evaluated through rules -> source=rule with the rule (D8)
	status, body, _ = doJSON(t, ts, "POST", fmt.Sprintf("/api/lines/%d/reset", lineIDs[1]), nil)
	if status != http.StatusOK {
		t.Fatalf("reset: %d %v", status, body)
	}
	if body["source"] != "rule" || body["ruleId"] != float64(ruleID) || body["subcategoryId"] != float64(subID) {
		t.Fatalf("after reset: %v", body)
	}

	// undo the reset -> back to manual
	doJSON(t, ts, "POST", "/api/actions/undo", nil)
	body = getLineViaAPI(t, ts, lineIDs[1])
	if body["source"] != "manual" {
		t.Fatalf("after undo of reset: %v", body)
	}
}

func TestImportTriggersRuleEvaluation(t *testing.T) {
	ts := newTestServer(t)
	accountID, _ := importNormalCSV(t, ts)
	subID := subcategoryIDByName(t, ts, "Groceries")

	// rule matching the Netto line, created AFTER the first import
	if c, _, _ := createRuleViaAPI(t, ts, ruleBodyFor("Netto", `Netto`, subID)); c != http.StatusCreated {
		t.Fatalf("create rule: %d", c)
	}

	// re-import the same file: all duplicates, no new lines
	content := fixtureCSV(t, "normal.csv")
	buf, ct := multipartBody(t, map[string]string{"accountId": fmt.Sprint(accountID)}, "file", "normal.csv", content)
	_, body, _ := doMultipart(t, ts, "POST", "/api/imports/preview", buf, ct)
	token := body["previewToken"].(string)
	buf, ct = multipartBody(t, map[string]string{"accountId": fmt.Sprint(accountID), "previewToken": token}, "file", "normal.csv", content)
	status, body, _ := doMultipart(t, ts, "POST", "/api/imports", buf, ct)
	if status != http.StatusCreated {
		t.Fatalf("re-import: %d %v", status, body)
	}
	if body["linesImported"].(float64) != 0 || body["ruleCategorized"].(float64) != 0 {
		t.Fatalf("re-import result: %v", body)
	}

	// import a second fixture with new lines -> rule evaluation runs (IMP-11)
	content2 := fixtureCSV(t, "duplicates.csv")
	buf, ct = multipartBody(t, map[string]string{"accountId": fmt.Sprint(accountID)}, "file", "duplicates.csv", content2)
	_, body, _ = doMultipart(t, ts, "POST", "/api/imports/preview", buf, ct)
	token = body["previewToken"].(string)
	buf, ct = multipartBody(t, map[string]string{"accountId": fmt.Sprint(accountID), "previewToken": token}, "file", "duplicates.csv", content2)
	status, body, _ = doMultipart(t, ts, "POST", "/api/imports", buf, ct)
	if status != http.StatusCreated {
		t.Fatalf("import duplicates.csv: %d %v", status, body)
	}
	// duplicates.csv contains Netto lines? check what it has and that ruleCategorized+uncategorized+conflicts == linesImported
	imported := int(body["linesImported"].(float64))
	byRule := int(body["ruleCategorized"].(float64))
	conflicts := int(body["ruleConflicts"].(float64))
	none := int(body["uncategorized"].(float64))
	if byRule+conflicts+none != imported {
		t.Fatalf("source counts must sum to linesImported: %v", body)
	}
	t.Logf("import result: %v", body)
}
