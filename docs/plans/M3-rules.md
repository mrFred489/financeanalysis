# M3 – Rules: implementation plan

Reference: [PRD.md](../../PRD.md) section 11, milestone M3.

## Goal

Regex rules that categorize lines automatically: rule CRUD with server-side live preview and edit diff, conflict detection and resolution, re-evaluation of affected lines on every rule change, "create rule from this line" on the categorize page, and rule evaluation on import. All rule mutations are undoable via the M2 action log.

Requirements covered (from PRD section 7): RULE-1..9, CATZ-7 (rule slot), IMP-11 (rule evaluation on import), UNDO-1 (rule actions), plus the M2 "reset to rules" semantics upgrade (M2 plan D4).

## Decisions

| # | Decision |
|---|---|
| D1 | New tables come via migration `00005_m3_rules.sql`: `rule`, `rule_account`, `conflict_resolution` (PRD section 6). `transfer_link`/`transfer_rejection` stay deferred to M4. |
| D2 | Rule evaluation lives in a dedicated package `internal/rules`: `Evaluate(ctx, db, lineIDs...)` applies the PRD §7.5 precedence (manual → transfer → rules → none/conflict) and returns the changes made, so callers can record them in the action log. Evaluation is pure Go (RE2 `regexp`), not SQL `REGEXP`, so previews, diffs and re-evaluation share one code path. |
| D3 | The SQLite `REGEXP` function is **not** needed; all matching happens in Go. This avoids registering custom SQL functions and keeps one source of truth for matching semantics (case-sensitive RE2, `(?i)` opt-in). |
| D4 | Rule mutations (create/edit/enable/disable/delete) are single endpoints that: begin tx → mutate `rule`/`rule_account` → re-evaluate all affected lines → `actions.Record` with snapshots of **both** the rule row and every changed `statement_line` row → commit. One undoable action per mutation (RULE-5, UNDO-1). |
| D5 | "Affected lines" for re-evaluation = all lines that currently have `source IN ('rule','conflict','none')` (i.e. not manual/transfer) **and** match the old or new pattern, plus lines whose `rule_id` pointed at the changed rule. This is computed by scanning candidate lines in Go (fast at ≤40k lines) rather than complex SQL. |
| D6 | Live preview (`POST /api/rules/preview`) takes the rule definition **without saving** and returns lines grouped by effect (RULE-3): `willCategorize`, `alreadyThisRule`, `willConflict`, `unaffectedManual`, `unaffectedTransfer`, each with count + sample line IDs. Edit diff (`POST /api/rules/{id}/diff`) compares the stored rule vs the submitted one and returns `added`, `removed`, `unchanged` line IDs, plus for removed lines what they will become (RULE-4). Both are read-only. |
| D7 | Conflict resolution: `POST /api/lines/{id}/resolve-conflict` body `{ "ruleId": number }` stores a `conflict_resolution` row, re-evaluates the line (it becomes `source='rule'` with that rule), and is one undoable action. `DELETE /api/lines/{id}/resolve-conflict` removes the remembered choice (RULE-9 drops it automatically when the rule no longer matches). Manual categorization of a conflicted line clears any `conflict_resolution` for it. |
| D8 | "Reset to rules" (`POST /api/lines/{id}/reset`, built in M2 with placeholder semantics) is upgraded: it clears the manual assignment **and any `conflict_resolution`**, then re-evaluates the line through the rules engine. Action kind stays `reset`. |
| D9 | "Create rule from this line" on the categorize page (`r` hotkey) navigates to `/rules/new?fromLine=<id>`; the rules form prefills pattern = escaped message, account filter = the line's account, direction from the sign of the amount, and a suggested name from the message prefix (RULE-1). |
| D10 | `GET /api/rules` returns each rule with a live `matchCount` (count of lines it currently categorizes + conflicts it participates in), computed during evaluation, not stored. Sortable/filterable server-side by name/pattern/enabled/subcategory (RULE-8). |
| D11 | Import commit (M1 `imports.Commit`) calls `rules.Evaluate` on the new lines after insert (IMP-11). The evaluation is **not** recorded in the action log (it is part of the import, which has its own undo, UNDO-4). |
| D12 | The `rule` entity is added to `actions.entityColumns` so undo can snapshot/restore rule rows; `rule_account` rows are snapshotted as `rule_account` entity with a composite key encoded as `entity_id = rule_id * 100000 + account_id` (account IDs are small), or more cleanly: store them as JSON in a single `rule` snapshot (see step 2). |

## Current state (from M2)

- `action_log`/`action_change` exist; `internal/actions` engine with `Record`/`Undo`/`Redo`/`List`/`Prune`, whole-row JSON snapshots, LIFO cap 200.
- `statement_line` has `subcategory_id`, `source` (`none`/`rule`/`manual`/`transfer`/`conflict`), `rule_id`, `spread_months`.
- Categorize page at `/categorize` with hotkeys, picker, undo/redo, spread, reset (placeholder semantics), and disabled slots for "create rule from this line" and "resolve conflict".
- `/rules` is a placeholder page.
- `GET /api/lines` supports `source` as comma-separated list including `conflict`.
- Import commit sets `source='none'` on all new lines (no rule evaluation yet).
- No `rule`, `rule_account`, `conflict_resolution` tables yet.

## Steps

### 1. Migration `00005_m3_rules.sql` + sqlc queries

Tables (STRICT):

- `rule(id INTEGER PK, name TEXT NOT NULL, pattern TEXT NOT NULL, direction TEXT NOT NULL DEFAULT 'both' CHECK(direction IN ('in','out','both')), subcategory_id INTEGER NOT NULL REFERENCES subcategory(id), enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)), created_at TEXT NOT NULL DEFAULT (strftime(...)), updated_at TEXT NOT NULL DEFAULT (strftime(...)))`
- `rule_account(rule_id INTEGER NOT NULL REFERENCES rule(id) ON DELETE CASCADE, account_id INTEGER NOT NULL REFERENCES account(id) ON DELETE CASCADE, PRIMARY KEY (rule_id, account_id))`
- `conflict_resolution(line_id INTEGER PK REFERENCES statement_line(id) ON DELETE CASCADE, rule_id INTEGER NOT NULL REFERENCES rule(id), created_at TEXT NOT NULL DEFAULT (strftime(...)))`
- Index on `statement_line(rule_id)` for "lines dropped by this rule" lookups.

sqlc queries to add in `internal/db/queries/`:
- Rules: `create_rule.sql`, `get_rule.sql`, `list_rules.sql` (with filters), `update_rule.sql`, `delete_rule.sql`, `set_rule_enabled.sql`, `replace_rule_accounts.sql` (delete all + insert new in tx), `list_rule_accounts.sql`.
- Rule accounts: `insert_rule_account.sql`, `delete_rule_accounts_for_rule.sql`, `list_accounts_for_rule.sql`.
- Conflict resolutions: `upsert_conflict_resolution.sql`, `get_conflict_resolution.sql`, `delete_conflict_resolution.sql`, `list_conflict_resolutions.sql`, `delete_conflict_resolutions_for_rule.sql` (when a rule is deleted).
- Line queries: `list_lines_by_rule.sql` (lines with `rule_id = ?`), `list_candidate_lines_for_rules.sql` (source IN ('none','rule','conflict')), `list_conflicted_lines.sql` (source='conflict', with their matching rules as JSON), `get_line_with_rule.sql` (line + rule name for display), `update_line_source.sql` (generic: sets subcategory_id, source, rule_id — used by the evaluator).

**Done when:** `task generate` produces clean sqlc output; migration test passes (empty → latest, integrity + FK checks).

### 2. Rule engine `internal/rules`

- `type Rule struct { ID int64; Name, Pattern, Direction string; SubcategoryID int64; Enabled bool; AccountIDs []int64 }` — loaded from DB.
- `Compile(pattern string) (*regexp.Regexp, error)` — RE2, case-sensitive by default; returns the compiler error for RULE-2.
- `Matches(r Rule, line Line) bool` — checks direction (amount sign) and account filter (empty = all), then `regexp.MatchString(line.Message)`.
- `EvaluateLines(ctx, db, lineIDs []int64) ([]actions.Change, error)` — for each line: skip if `source IN ('manual','transfer')`; find all enabled matching rules; apply precedence (0 → `none`, 1 → `rule`+subcategory, >1 → conflict_resolution if valid else `conflict`). Returns `actions.Change` entries (before/after snapshots) for every line whose `source`/`subcategory_id`/`rule_id` changed. Also drops stale `conflict_resolution` rows (RULE-9) and returns those as changes too (or deletes them in the same tx — decide: delete, since they are derived state).
- `EvaluateAll(ctx, db) ([]actions.Change, error)` — convenience: all candidate lines (source not manual/transfer). Used on import and on demand.
- `Preview(ctx, db, rule Rule) (*PreviewResult, error)` — RULE-3: run the rule against all candidate lines **hypothetically** (without writing) and group: `willCategorize` (currently `none` and matches only this rule), `alreadyThisRule` (currently `rule` with this rule id), `willConflict` (matches this rule + ≥1 other enabled rule, no valid resolution), `unaffectedManual`, `unaffectedTransfer`. Counts + up to 5 sample line IDs per group.
- `Diff(ctx, db, ruleID int64, newRule Rule) (*DiffResult, error)` — RULE-4: compare old vs new matching sets; `added` = lines the new rule matches that the old didn't (and aren't manual/transfer), `removed` = lines the old matched that the new doesn't, `unchanged` = intersection. For each removed line, compute what it will become after re-evaluation (other rule / conflict / none).
- Unit tests: precedence (manual/transfer untouched), 0/1/many matches, conflict_resolution honored and dropped when stale (RULE-9), direction filter, account filter (empty = all), case-sensitivity with `(?i)`, invalid regex error, preview groups, diff added/removed/unchanged, evaluate-all idempotence.

**Done when:** `go test ./internal/rules/...` passes.

### 3. Actions engine extension

- Add `EntityRule = "rule"` and `EntityConflictResolution = "conflict_resolution"` to `actions.entityColumns` with their column lists, so `Record`/`Undo`/`Redo` can snapshot rule and conflict_resolution rows. For `rule_account`, snapshot the whole rule row including its account IDs as a JSON array (i.e. the `Rule` struct's `After` includes `AccountIDs`), stored as part of the `rule` entity change — undo restores the rule row and the caller re-syncs `rule_account` from the snapshot. Simpler alternative: store `rule_account` rows as separate changes with `entity="rule_account"`, `entity_id` = a synthetic key; but since undo restores by `WHERE id = ?`, and `rule_account` has no single `id` column, the cleanest approach is: **the `rule` change's `after_json` includes `accountIds`, and a custom restore hook re-syncs `rule_account`**. Implement the hook in `applySnapshot` for entity `rule`.
- Unit tests: undo of rule create restores lines + removes rule; undo of rule edit restores old pattern + old line assignments; undo of conflict resolution restores conflict state.

**Done when:** `go test ./internal/actions/...` passes with the new entities.

### 4. HTTP API endpoints

All under `/api/`, same error shape as M1/M2. Each mutation endpoint: validate → begin tx → mutate → re-evaluate affected lines → `actions.Record` → commit.

| Method | Route | Description |
|---|---|---|
| `GET` | `/api/rules` | List rules with matchCount, filterable by `enabled`, `subcategoryId`, `search` (name/pattern substring), sortable by `name`/`pattern`/`subcategory`/`matchCount`/`createdAt` (RULE-8). |
| `POST` | `/api/rules` | Create rule (RULE-1). Body `{ name, pattern, direction, subcategoryId, accountIds?, enabled? }`. Validates regex (RULE-2). Re-evaluates all candidate lines. One undoable action `rule_create`. |
| `GET` | `/api/rules/{id}` | Rule detail incl. account IDs and current match count. |
| `PUT` | `/api/rules/{id}` | Edit rule. Re-evaluates affected lines (old matches + new matches). One undoable action `rule_edit` (RULE-5). |
| `POST` | `/api/rules/{id}/toggle` | Enable/disable. Re-evaluates lines of this rule. Undoable `rule_toggle`. |
| `DELETE` | `/api/rules/{id}` | Delete rule + its `rule_account` + `conflict_resolution` rows; re-evaluates dropped lines (RULE-6). Undoable `rule_delete`. |
| `POST` | `/api/rules/preview` | RULE-3 live preview: body = rule definition (no id needed). Returns grouped counts + samples. Read-only. |
| `POST` | `/api/rules/{id}/diff` | RULE-4 edit diff: body = new rule definition. Returns added/removed/unchanged + removed lines' future state. Read-only. |
| `GET` | `/api/conflicts` | List all conflicted lines (source='conflict') with their matching rules (id + name + pattern) and any existing resolution. |
| `POST` | `/api/lines/{id}/resolve-conflict` | Body `{ "ruleId": number }`. Stores `conflict_resolution`, sets line to `source='rule'`. Undoable `resolve_conflict` (RULE-7). `409` if line is not in conflict or rule doesn't match. |
| `DELETE` | `/api/lines/{id}/resolve-conflict` | Removes remembered resolution; line returns to `conflict`. Undoable. |

Also modify existing endpoints:
- `POST /api/lines/{id}/reset` — upgrade to real re-evaluation (D8): clears manual assignment + `conflict_resolution`, then `rules.EvaluateLines([id])`.
- `POST /api/lines/{id}/categorize` and bulk — when manually categorizing a conflicted line, delete its `conflict_resolution` row in the same tx (D7).
- `POST /api/imports` commit — after inserting lines, call `rules.EvaluateAll` on the new line IDs (IMP-11, D11); result screen adds "auto-categorized by rules" count (IMP-12).

**Done when:** Go integration tests (httptest + temp SQLite) pass: rule CRUD round trip, regex validation error, preview groups, diff, toggle re-evaluation, delete re-evaluation, conflict resolve + undo, reset-to-rules re-evaluation, import triggers evaluation.

### 5. Frontend – Rules page (`/rules`)

Replace the placeholder in `web/src/routes/rules/+page.svelte`.

- **List (RULE-8):** table of rules: name, pattern (monospace), direction badge, target subcategory (colored chip), account filter (or "all"), enabled toggle, match count, created/updated. Sortable columns, filter by enabled/subcategory/search box. Row actions: Edit, Enable/Disable, Delete (with confirm).
- **Create/Edit form** (`/rules/new`, `/rules/{id}/edit`): fields name, pattern (with live regex validation error from server), direction select, subcategory picker, account multi-select (empty = all), enabled checkbox.
  - **Live preview (RULE-3):** debounced `POST /api/rules/preview` on every field change; panel shows the five groups with counts and expandable sample lines.
  - **Edit diff (RULE-4):** when editing, a toggle switches preview to diff mode via `POST /api/rules/{id}/diff`; shows added/removed/unchanged with removed lines' future state.
  - Save → `POST`/`PUT` → redirect to list. Cancel → back.
- **Conflicts section** (`/rules/conflicts` or a tab): list of conflicted lines, each showing message, amount, date, account, and the matching rules as buttons (click = resolve with that rule, RULE-7). Each rule button has an "Edit rule" link. Also a "Categorize manually" link to `/categorize?focus=<id>&source=conflict`.
- Deep link: `/rules/new?fromLine=<id>` prefills from the line (D9).

**Done when:** Vitest tests pass for the preview/diff panel components and the form validation; manual check that editing a rule updates live preview counts and saving re-categorizes lines.

### 6. Frontend – Categorize page integration (CATZ-7)

- Enable the "Create rule from this line" slot: `r` hotkey → `/rules/new?fromLine=<focusedLineId>` (D9).
- Enable the "Resolve conflict" slot on conflicted focused lines: shows matching rules inline; picking one calls `POST /api/lines/{id}/resolve-conflict` and refetches.
- "Reset to rules" (`Backspace`) now actually re-evaluates through rules (backend change, D8); the UI label stays.

**Done when:** Playwright can press `r` on a focused line, land on the prefilled rule form, save, and see the line categorized by the new rule.

### 7. Import integration (IMP-11, IMP-12)

- Backend: `imports.Commit` calls `rules.EvaluateAll` on new lines after insert; result includes `ruleCategorized` count.
- Frontend: import result screen adds "Auto-categorized by rules: N" line.

**Done when:** Playwright: import fixture → create a rule that matches some lines → re-import (or import another file) → result screen shows the count, lines have `source='rule'`.

### 8. i18n

Add `rules.*` and `conflicts.*` namespaces to `web/messages/en.json` (list, form, preview groups, diff, conflicts, toasts). Remove `placeholder.m3` from the rules page.

### 9. Tests

**Go unit:**
- `rules`: precedence, 0/1/many matches, conflict_resolution honored + dropped when stale, direction, account filter, case-sensitivity, invalid regex, preview groups, diff, idempotence (step 2).
- `actions`: new entities (rule, conflict_resolution) undo/redo round trips (step 3).

**Go integration (httptest + temp dir):**
- All endpoints in step 4, including 409 cases (resolve non-conflict, invalid regex), import-triggers-evaluation, reset-to-rules re-evaluation.

**Frontend:**
- Vitest: rules list sorting/filtering, form validation, preview/diff panel rendering, conflicts list.
- Playwright e2e against the native binary with temp `FA_DATA_DIR`: import fixture CSV → go to `/rules/new` → create a rule matching some lines → see live preview counts → save → statements view shows lines with `source='rule'` → edit rule to narrow pattern → diff shows removed lines → save → removed lines become `none` → create a second rule overlapping → conflict appears in `/rules/conflicts` → resolve conflict → line becomes `rule` → `Ctrl+Z` on categorize page undoes the resolution → reset-to-rules on a manual line re-evaluates it → delete rule → its lines re-evaluated → undo restores.

**Done when:** `task test`, `task lint`, `task check:generated` pass and the Playwright flow above is green.

## Definition of done (M3)

- [ ] `/rules` lists all rules with pattern, filters, target, enabled, match count; sortable/filterable; create/edit/delete/toggle work.
- [ ] Live preview (RULE-3) shows grouped counts while editing; edit diff (RULE-4) shows added/removed/unchanged.
- [ ] Invalid regex is rejected with the compiler error before saving (RULE-2).
- [ ] Saving a rule re-evaluates affected lines atomically and is one undoable action; undo restores both rule and lines (RULE-5).
- [ ] Conflicts are detected (>1 matching rule, no valid resolution) and listed with matching rules; resolving stores `conflict_resolution` and is undoable (RULE-7); stale resolutions are dropped automatically (RULE-9).
- [ ] "Create rule from this line" on the categorize page prefills the form (RULE-1); "reset to rules" re-evaluates through rules.
- [ ] Import runs rule evaluation on new lines and the result screen shows the count (IMP-11/12).
- [ ] `task test`, `task lint`, `task check:generated` and the Playwright e2e flow pass.

## Out of scope for M3

Transfers (M4: detection, review, link/unlink); charts (M5); action history panel UI (M6); additional rule types like "contains"/"starts with" (RULE-10, Later); categorization suggestions (Later).

## Risks

| Risk | Mitigation |
|---|---|
| Rule evaluation over 40k lines is slow on every rule save | Evaluate only candidate lines (source not manual/transfer) and only those matching old/new pattern; Go RE2 is linear-time; benchmark in tests (NFR-3: import <5s incl. evaluation). |
| Regex catastrophic backtracking | RE2 is linear by design (NFR-8); still cap pattern length (e.g. 512 chars) and reject `regexp.Compile` errors. |
| Undo of rule changes needs to restore `rule_account` rows, which have no single `id` | Snapshot the rule row including `accountIds` array; the restore hook for entity `rule` re-syncs `rule_account` (delete + insert) from the snapshot (D12). |
| Conflict resolution staleness when a rule is edited | `EvaluateLines` drops `conflict_resolution` rows whose rule no longer matches (RULE-9); deletion of a rule cascades `conflict_resolution` (FK ON DELETE CASCADE). |
| Preview/diff on large datasets feels laggy in the UI | Debounce 300 ms; preview is read-only and paginated samples (max 5 per group); full lists available on save result. |
| Import evaluation + undo interaction | Import evaluation is part of the import transaction and not in the action log (UNDO-4); import undo deletes lines, which cascades `conflict_resolution`. |
