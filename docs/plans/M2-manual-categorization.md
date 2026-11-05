# M2 – Manual categorization: implementation plan

Reference: [PRD.md](../../PRD.md) section 11, milestone M2.

## Goal

A keyboard-first workspace for categorizing lines manually: the categorization page (line list with one focused line, subcategory palette with hotkeys, type-ahead picker), per-line spread months, a persistent LIFO undo/redo action log, and working deep links from the statements view and import result ("Categorize these" / "Categorize now").

Requirements covered (from PRD section 7): CATZ-1..10 (+ CATZ-11 as Should), UNDO-1..4, STMT-6, VIZ-7, CAT-10 (hotkey editing UI already landed in M1 settings; this milestone consumes the hotkeys).

## Decisions

| # | Decision |
|---|---|
| D1 | New tables come via migration `00004_m2_actions.sql`: only `action_log` and `action_change` (PRD section 6). `rule`, `conflict_resolution`, `transfer_link`, `transfer_rejection` stay deferred to M3/M4. |
| D2 | The undo engine lives in a dedicated package `internal/actions`: `Record` writes the mutation and its `action_change` rows in **one transaction**; `Undo`/`Redo` replay `before_json`/`after_json` row snapshots and are strictly LIFO with a cap of 200 entries (UNDO-3). Imports keep their own undo (UNDO-4). |
| D3 | Categorization mutations are small dedicated endpoints (table in step 3), not a generic PATCH, so each maps to exactly one `action_log.kind`. |
| D4 | "Reset to rules" in M2 clears the manual assignment (`source='none'`, `subcategory_id=NULL`) because no rules exist yet. The endpoint and action kind stay unchanged; M3 upgrades the semantics to rule re-evaluation. |
| D5 | The categorize page reuses `GET /api/lines` + `statementFilters.ts` for its list and URL state, plus a `focus=<lineId>` URL param so STMT-6 row links work. The default filter is `source=none,conflict`, newest first (CATZ-1). |
| D6 | `GET /api/lines` `source` param is extended to accept a comma-separated list (single values keep working), so the categorize default can request `none,conflict` in one query. |
| D7 | Keyboard map per CATZ-9: `↑/↓` or `k/j` move focus, subcategory hotkeys assign, `Space` opens the picker, `s` sets spread, `Backspace` resets to rules, `Ctrl+Z` undo, `Ctrl+Shift+Z` redo, `/` focuses filter, `?` help overlay. Reserved keys (navigation, undo, picker, help) cannot be used as subcategory hotkeys. |
| D8 | `action_change` stores whole-row JSON snapshots (`before_json`/`after_json`) per affected row of `statement_line` (and, later, `subcategory`/`rule`). Undo restores rows by ID, so it works across reloads and restarts (UNDO-2). |
| D9 | Multi-select (`Shift+↑/↓`, CATZ-11) assigns one subcategory to all selected lines via one bulk endpoint, recorded as **one** undoable action. |

## Current state (from M1)

- `statement_line` already has `subcategory_id`, `source`, `rule_id`, `spread_months` (migrations 00002/00003); all M1 lines have `source='none'`.
- `GET /api/lines` supports filters (incl. `source`, `spreadGt`), server-side sort/pagination, totals; `web/src/lib/statementFilters.ts` serializes filter state to the URL.
- `/categorize` is a placeholder page; the statements view's "Categorize these" button and the import result's "Categorize now" button already link to it.
- Settings already lets the user edit subcategory hotkeys (CAT-10).
- No `action_log`/`action_change` tables yet; no mutation endpoints for lines yet.

## Steps

### 1. Migration `00004_m2_actions.sql` + sqlc queries

Tables (STRICT):

- `action_log(id INTEGER PK, kind TEXT NOT NULL, description TEXT NOT NULL, created_at TEXT NOT NULL DEFAULT (strftime(...)), undone_at TEXT)`
- `action_change(action_id INTEGER NOT NULL REFERENCES action_log(id) ON DELETE CASCADE, entity TEXT NOT NULL, entity_id INTEGER NOT NULL, before_json TEXT NOT NULL, after_json TEXT NOT NULL, PRIMARY KEY (action_id, entity, entity_id))`
- Index on `action_log(id)` (LIFO order = id order).

sqlc queries to add in `internal/db/queries/`:
- Actions: `insert_action.sql`, `insert_action_change.sql`, `list_actions.sql` (latest first, with change counts), `get_action.sql` (with its changes), `mark_action_undone.sql`, `mark_action_redone.sql`, `delete_oldest_actions.sql` (keep newest 200).
- Line mutations: `set_line_subcategory.sql` (sets `subcategory_id`, `source='manual'`, clears `rule_id`), `set_line_spread.sql`, `reset_line_to_rules.sql` (M2 semantics: `subcategory_id=NULL`, `source='none'`, `rule_id=NULL`), `get_line_for_action.sql` (full row as JSON for snapshots).

**Done when:** `task generate` produces clean sqlc output; migration test passes (empty → latest, integrity + FK checks).

### 2. Undo engine `internal/actions`

- `type Change struct { Entity string; EntityID int64; Before, After any }` — Before/After are full row structs, serialized to JSON for storage.
- `Record(ctx, db, kind, description string, changes []Change) (actionID int64, err error)` — callers pass the changes they made; `Record` inserts the `action_log` row + changes. The handler wraps mutation + `Record` in one transaction.
- `Undo(ctx, db) (*Action, error)` — latest action with `undone_at IS NULL`; applies each change's `before_json` back to its row (UPDATE by entity/id), sets `undone_at`. Returns the action for the response.
- `Redo(ctx, db) (*Action, error)` — latest action with `undone_at NOT NULL`; applies `after_json`, clears `undone_at`.
- `List(ctx, db, limit)` — for tests now and the M6 history panel later (UNDO-5).
- `Prune(ctx, db)` — after each Record, delete all but the newest 200 actions (cascade deletes their changes).
- Unit tests: LIFO order, cap at 200, undo→redo→undo round trip, bulk action restores every row, persistence across close/reopen of the DB file, undo with empty stack returns a typed `ErrNothingToUndo`.

**Done when:** `go test ./internal/actions/...` passes.

### 3. HTTP API endpoints

All under `/api/`, same error shape as M1. Each mutation endpoint: validate → begin tx → mutate → `actions.Record` → commit → return the affected line(s).

| Method | Route | Description |
|---|---|---|
| `POST` | `/api/lines/{id}/categorize` | Body `{ "subcategoryId": number }` → `source='manual'` (CATZ-4). Undoable action `categorize`. |
| `POST` | `/api/lines/categorize` | Bulk: `{ "lineIds": [...], "subcategoryId": number }` → one undoable action `categorize_bulk` (CATZ-11). |
| `POST` | `/api/lines/{id}/spread` | Body `{ "months": 1..36 }` → sets `spread_months` (VIZ-7, CATZ-7). Undoable action `spread`. |
| `POST` | `/api/lines/{id}/reset` | Clears manual assignment (D4). Undoable action `reset`. |
| `GET` | `/api/actions` | Recent actions with descriptions + undone flag (UNDO-2, feeds M6 panel). |
| `POST` | `/api/actions/undo` | Undo latest action (UNDO-3). `409` when nothing to undo. |
| `POST` | `/api/actions/redo` | Redo latest undone action. `409` when nothing to redo. |

Also:
- Extend the `source` filter of `GET /api/lines` to a comma-separated list (D6); update `lines.go` parsing and add an integration test. Statements UI is unaffected (it sends a single value).

**Done when:** Go integration tests (httptest + temp SQLite) pass: categorize → undo → redo round trip, bulk categorize + undo restores all rows, spread validation (1..36), reset, action list, 409 on empty undo/redo, and the `source=none,conflict` list filter.

### 4. Frontend – Categorize page (`/categorize`)

Replace the placeholder in `web/src/routes/categorize/+page.svelte`. Reuse `statementFilters.ts` for the list query and URL state; add `focus` param.

- **List (CATZ-1):** `GET /api/lines` with the URL filters; default when no params: `source=none,conflict`, `sort=date`, `order=desc`. Categorized lines stay visible, dimmed, until refresh (CATZ-4).
- **Focus (CATZ-2):** one highlighted row; a detail pane always shows the focused line's account, full message, amount, balance, dates, current source/subcategory. Focus is kept by line ID in the URL (`&focus=<id>`) and clamped to the list after refetch.
- **Palette (CATZ-3):** subcategories grouped by category, colored by category hue, hotkey badge on each button. Click or hotkey assigns and moves focus to the next line (CATZ-4).
- **Picker (CATZ-5):** `Space` opens a type-ahead fuzzy picker over all subcategories.
- **Multi-select (CATZ-11):** `Shift+↑/↓` extends the selection; assigning applies to all selected lines via the bulk endpoint.
- **Actions on the focused line (CATZ-7):** spread dialog (`s`, 1–36 months), reset to rules (`Backspace`). Slots for "create rule from this line", "resolve conflict" and "jump to transfer counterpart" are rendered disabled with a tooltip until M3/M4.
- **Shortcut discovery (CATZ-8):** persistent hint bar at the bottom + `?` overlay listing all shortcuts (CATZ-8, NFR-6: labels on buttons too, not only colors/badges).
- **Undo/redo:** `Ctrl+Z` / `Ctrl+Shift+Z` call the endpoints, then refetch the list; focus returns to the first affected line (CATZ-6).
- Keyboard handling is isolated in a testable module (`web/src/lib/categorizeKeys.ts`) mapping key events to actions, with the reserved-key list (D7).

**Done when:** Vitest tests pass for `categorizeKeys.ts` (mapping, reserved keys, multi-select) and focus movement; manual check that assign → focus advances → undo restores the previous subcategory.

### 5. Deep links (STMT-6)

- Statements view: "Categorize these" already forwards the current filter query string; make each row link to `/categorize?<same filters>&focus=<lineId>`.
- Import result: "Categorize now" links to `/categorize?source=none`.

**Done when:** Playwright can click a statements row and land on the categorize page with that line focused.

### 6. i18n

Add a `catz.*` namespace to `web/messages/en.json` (list, palette, picker, actions, spread dialog, shortcut overlay, hint bar, undo/redo toasts) and remove the `placeholder.m2` usage from the categorize page.

### 7. Tests

**Go unit:**
- `actions`: LIFO, 200-cap pruning, undo/redo round trip, bulk restore, persistence across reopen, empty-stack errors (step 2).

**Go integration (httptest + temp dir):**
- All endpoints in step 3, including the `source` list filter on `/api/lines` and 409 cases.

**Frontend:**
- Vitest: `categorizeKeys.ts`, focus movement/clamping, picker fuzzy filter, URL serialization with `focus`.
- Playwright e2e against the native binary with a temp `FA_DATA_DIR`: import a fixture CSV from `testdata/fixtures/csv/` → go to `/categorize` → assign a subcategory with a hotkey → focus advances → `Ctrl+Z` reverts → `Ctrl+Shift+Z` redoes → set spread to 12 → select 3 lines with `Shift+↓` and bulk-assign → undo → verify via `/api/lines` and the statements view.

**Done when:** `task test`, `task lint`, `task check:generated` pass and the Playwright flow above is green.

## Definition of done (M2)

- [ ] `/categorize` lists lines (default: uncategorized + conflicts, newest first) with filters in the URL; deep links from statements rows and the import result focus the right line.
- [ ] Hotkey or palette click assigns a subcategory (`source='manual'`) and moves focus to the next line; categorized lines stay visible dimmed.
- [ ] Type-ahead picker works when there are more subcategories than hotkeys.
- [ ] `Shift+↑/↓` multi-select + bulk assign works and is a single undoable action.
- [ ] `Ctrl+Z` / `Ctrl+Shift+Z` undo/redo the last action(s) persistently (survives reload); the stack is LIFO and capped at 200.
- [ ] Spread months (1–36) can be set per line from the categorize page; reset-to-rules clears a manual assignment.
- [ ] `task test`, `task lint`, `task check:generated` and the Playwright e2e flow pass.

## Out of scope for M2

Rules including "create rule from line", conflict resolution and the conflicts list (M3); transfer detection, linking and counterpart jump (M4); charts (M5); the action history panel UI (M6, UNDO-5 — the `GET /api/actions` endpoint is built now); categorization suggestions (Later).

## Risks

| Risk | Mitigation |
|---|---|
| Hotkey collisions with reserved keys or between subcategories | Reserved-key list enforced in `categorizeKeys.ts`; `hotkey` is UNIQUE in the schema; the picker is the fallback (PRD open question 1). |
| Undo breaks when the `statement_line` schema changes in M3/M4 | Whole-row JSON snapshots restore only known columns; migrations must not drop/rename columns that actions reference (note in `docs/migrations.md` checklist). |
| Focus jumps around when the list refetches after an action | Focus is tracked by line ID, not index; clamped to the fetched page, and set to the first affected line after undo (CATZ-6). |
| `source` list filter change breaks the statements UI | Single values remain valid (comma-split of one element); statements tests keep passing. |
| Per-keystroke server round trips feel slow | Actions are discrete key presses (no type-ahead against the server); NFR-3 (<200 ms) is trivially met at 40k lines with the existing indexes. |
