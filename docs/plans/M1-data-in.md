# M1 – Data in: implementation plan

Reference: [PRD.md](../../PRD.md) section 11, milestone M1.

## Goal

A working "data in" pipeline: the user can set up the app (welcome wizard, accounts, subcategories), import bank CSV files with preview/dedup/checks, undo the latest import, browse all imports, and view all statement lines in a filterable table.

Requirements covered (from PRD section 7): SETUP-1..6, IMP-1..14, CAT-1..3 + CAT-5 + CAT-7..8, STMT-1..6.

## Decisions

| # | Decision |
|---|---|
| D1 | New tables are added via migration `00002_m1_data_in.sql` (one logical change per file; account, subcategory, import_batch, statement_line in one migration is acceptable since they are interdependent via FKs). |
| D2 | CSV parsing lives in a dedicated package `internal/csvparse` so it is unit-testable without HTTP. |
| D3 | Import is a two-phase HTTP flow: `POST /api/imports/preview` (multipart upload, returns preview JSON, nothing written) then `POST /api/imports` (commit, takes the preview payload + account ID). The preview payload is signed/validated server-side to prevent tampering between phases. |
| D4 | The statements view uses server-side filtering, sorting, and pagination via query params encoded in the URL. Default page size 50. |
| D5 | Rules and transfers are **not** in M1. On import commit, lines get `source = 'none'`. Transfer detection and rule evaluation are M3/M4. The `rule`, `transfer_link`, `conflict_resolution`, `action_log` tables are deferred to their milestones. |
| D6 | `spread_months` column is added now (default 1) since it belongs to `statement_line`, but the UI for setting it is M2. |

## Current state (from M0)

- Go server with `GET /api/status`, `POST /api/database`, SPA fallback.
- DB has `setting` and `category` (seeded with 4 categories).
- Frontend: home page with create-database button, layout shell, i18n catalog.
- sqlc configured; generated code in `internal/db/dbq`.

## Steps

### 1. Database migration `00002_m1_data_in.sql`

Tables (STRICT where applicable):

- `account(id INTEGER PK, name TEXT UNIQUE NOT NULL, account_number TEXT, sort_order INTEGER NOT NULL DEFAULT 0, archived INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL DEFAULT (datetime))`
- `subcategory(id INTEGER PK, category_id INTEGER NOT NULL REFERENCES category(id), name TEXT NOT NULL, color TEXT NOT NULL, icon TEXT, hotkey TEXT, sort_order INTEGER NOT NULL DEFAULT 0, UNIQUE(category_id, name), UNIQUE(hotkey))`
- `import_batch(id INTEGER PK, account_id INTEGER NOT NULL REFERENCES account(id), file_name TEXT NOT NULL, file_sha256 TEXT NOT NULL, imported_at TEXT NOT NULL DEFAULT (datetime), line_count INTEGER NOT NULL DEFAULT 0, duplicate_count INTEGER NOT NULL DEFAULT 0, undone_at TEXT)`
- `statement_line(id INTEGER PK, account_id INTEGER NOT NULL REFERENCES account(id), import_id INTEGER REFERENCES import_batch(id), booking_date TEXT NOT NULL, message TEXT NOT NULL, amount_minor INTEGER NOT NULL, balance_minor INTEGER NOT NULL, currency TEXT NOT NULL DEFAULT 'DKK', file_row INTEGER NOT NULL, seq INTEGER NOT NULL, occurrence INTEGER NOT NULL DEFAULT 1, subcategory_id INTEGER REFERENCES subcategory(id), source TEXT NOT NULL DEFAULT 'none' CHECK(source IN ('none','rule','manual','transfer','conflict')), rule_id INTEGER, spread_months INTEGER NOT NULL DEFAULT 1 CHECK(spread_months BETWEEN 1 AND 36))`
- Unique index on `statement_line(account_id, booking_date, amount_minor, balance_minor, message, occurrence)` for duplicate detection.
- Index on `statement_line(account_id, booking_date, seq)` for chronological listing.
- Index on `statement_line(subcategory_id)`, `statement_line(source)`, `import_batch(account_id)`.

Seed default subcategories per category (editable in the welcome wizard and settings):
- Income: Salary, Other income
- Fixed costs: Housing, Insurance, Subscriptions
- Variable spending: Groceries, Dining, Transport, Shopping, Leisure
- Savings: Savings transfer, Investment

Auto-generate shades of the category base hue for subcategory colors (CAT-5).

sqlc queries to add in `internal/db/queries/`:
- Accounts: `list_accounts.sql`, `create_account.sql`, `update_account.sql`, `delete_account.sql` (only if no lines), `get_account.sql`
- Subcategories: `list_subcategories.sql`, `create_subcategory.sql`, `update_subcategory.sql`, `delete_subcategory.sql` (with merge target param), `list_subcategories_by_category.sql`
- Imports: `create_import_batch.sql`, `get_import_batch.sql`, `list_import_batches.sql`, `mark_import_undone.sql`, `get_latest_non_undone_import.sql`, `insert_statement_line.sql`, `delete_lines_by_import.sql`, `count_lines_by_import.sql`, `list_statement_lines.sql` (with filter params), `count_statement_lines.sql` (with filters), `get_statement_line.sql`, `line_exists.sql` (duplicate check)
- Settings: existing `get_setting.sql`, `upsert_setting.sql`

**Done when:** `task generate` produces clean sqlc output; migration test passes (empty → latest, integrity + FK checks).

### 2. CSV parser package `internal/csvparse`

> **Update 2026-11-05 — format changed:** the bank's real CSV export does not match the layout assumed below. PRD §7.2 now specifies the actual 16-column format (no running balance, three dates, two account columns), and the parser/import/fixture work for it is planned in [csv-format-v2.md](csv-format-v2.md). Treat step 2, the parsing/checks parts of step 3, and the CSV fixtures in step 10 as superseded by that plan.

- `Parse(r io.Reader) ([]RawLine, []ParseError, error)`:
  - Strip UTF-8 BOM.
  - Split on `\n`, handle `\r\n`.
  - For each row: split on `;`. First field = date (`dd-mm-yyyy` → `YYYY-MM-DD`), last three = amount, balance, currency; join middle fields with `;` as message.
  - Parse Danish number format: `.` thousands, `,` decimal → integer minor units (øre). E.g. `-1.357,97` → `-135797`.
  - Skip empty rows.
  - Collect parse errors with row number + reason (IMP-3).
  - Reject non-DKK rows with a warning (IMP-4).
  - Assign `seq` = reverse of file order (newest first in file → chronological seq ascending, IMP-8a).
  - Assign `occurrence` = 1-based index among identical (date, amount, balance, message) tuples within the file.
- Unit tests with fixture CSVs covering: BOM, `;` in message, Danish number formats, parse errors, non-DKK, duplicate rows, empty file, CRLF.

**Done when:** `go test ./internal/csvparse/...` passes with all edge cases.

### 3. Import logic `internal/imports`

- `Preview(ctx, db, accountID, fileName, content) (*PreviewResult, error)`:
  - Parse CSV → raw lines + errors.
  - Compute SHA-256 of file content.
  - Check against existing imports for same SHA-256 → warning (IMP-9).
  - Duplicate detection: for each raw line, check the unique key against existing `statement_line` rows → count duplicates (IMP-6).
  - Balance continuity check within the file: `balance(row) = balance(row below) + amount(row)` → collect gap warnings (IMP-7).
  - Account sanity check against existing lines: oldest new row's `balance - amount` vs latest existing line's balance; newest end likewise → warn if mismatch (IMP-8).
  - Return: date range, new count, duplicate count, parse errors, warnings (balance gaps, account mismatch, duplicate SHA).
- `Commit(ctx, db, backupDir, accountID, fileName, content, previewToken) (*CommitResult, error)`:
  - Validate preview token.
  - Take backup (IMP-10).
  - In a transaction: insert `import_batch`, insert non-duplicate `statement_line` rows with seq/occurrence, set `source='none'`.
  - Return: lines imported, duplicates skipped, errors.
- `UndoLatest(ctx, db, backupDir) error` (IMP-13):
  - Find latest non-undone import batch (globally).
  - Take backup.
  - In a transaction: delete its `statement_line` rows, mark batch `undone_at`.
  - (Transfer links and conflict resolutions don't exist yet in M1; those deletions come in M4/M3.)

**Done when:** Go unit tests pass for preview (dedup, balance gaps, account mismatch, SHA warning) and commit/undo (round-trip).

### 4. HTTP API endpoints

All under `/api/`, JSON in/out, consistent error shape `{ "error": { "code", "message" } }`.

| Method | Route | Description |
|---|---|---|
| `GET` | `/api/status` | Extended: also returns `accountsConfigured` bool. |
| `POST` | `/api/database` | Existing; extended to accept optional seed accounts/subcategories from the welcome wizard. |
| `POST` | `/api/database/upload` | Upload existing DB file (multipart), validate schema version, back up + migrate if older, refuse if newer (SETUP-4). |
| `GET` | `/api/database/download` | `VACUUM INTO` to a temp file, stream as download (SETUP-5). |
| `POST` | `/api/database/replace` | Replace DB with uploaded file; automatic backup of current (SETUP-5). |
| `GET` | `/api/backups` | List backups in the backups dir. |
| `POST` | `/api/backups/{name}/restore` | Restore a backup (with automatic backup of current first). |
| `GET` | `/api/accounts` | List accounts (including archived). |
| `POST` | `/api/accounts` | Create account. |
| `PUT` | `/api/accounts/{id}` | Rename / archive / set sort order. |
| `DELETE` | `/api/accounts/{id}` | Delete only if no lines (SETUP-6). |
| `GET` | `/api/subcategories` | List all subcategories grouped by category. |
| `POST` | `/api/subcategories` | Create. |
| `PUT` | `/api/subcategories/{id}` | Update name/color/hotkey/order/move to another category (CAT-3). |
| `DELETE` | `/api/subcategories/{id}` | Merge-delete: requires `mergeIntoId` param; reassign lines (none yet in M1, but rules later) (CAT-4). |
| `GET` | `/api/categories` | List the 4 fixed categories with their subcategories. |
| `POST` | `/api/imports/preview` | Multipart: file + account ID. Returns preview JSON. Max body 50 MiB. |
| `POST` | `/api/imports` | Commit import (account ID + preview token). Returns result summary (IMP-12). |
| `POST` | `/api/imports/{id}/undo` | Undo latest import (IMP-13). |
| `GET` | `/api/imports` | Import history list (IMP-14). |
| `GET` | `/api/imports/{id}` | Import detail with line counts. |
| `GET` | `/api/lines` | Statements view: server-side filter/sort/paginate (STMT-2, STMT-3). Query params: `from`, `to`, `accounts`, `direction`, `amountMin`, `amountMax`, `message`, `categories`, `subcategories`, `source`, `importId`, `spreadGt`, `sort`, `order`, `page`, `pageSize`. Returns lines + totals (STMT-4). |
| `GET` | `/api/lines/{id}` | Single line detail. |
| `GET` | `/api/settings` | Get all settings (locale, etc.). |
| `PUT` | `/api/settings` | Update settings. |

Server changes:
- Increase `MaxBytesReader` to 50 MiB for `/api/imports/preview` and `/api/database/upload`.
- Add a route-specific body limit middleware.

**Done when:** Go integration tests (httptest + temp SQLite) pass for all endpoints.

### 5. Frontend – Welcome wizard (SETUP)

Routes:
- `/welcome` – shown when DB is missing (SETUP-1). Two options: **Create new database** / **Upload existing database** (SETUP-2).
- `/welcome/new` – wizard:
  1. Step 1: add one or more accounts (name, optional account number) (SETUP-3).
  2. Step 2: review/edit seeded subcategories per category (name, color preview) (SETUP-3).
  3. Step 3: finish → `POST /api/database` with accounts + subcategories → redirect to `/import`.
- `/welcome/upload` – file picker → `POST /api/database/upload` → validate → redirect to dashboard.

If DB exists on load → redirect to `/` (dashboard placeholder, or `/statements`).

**Done when:** Playwright test: fresh start → welcome → create new → wizard → land on import page.

### 6. Frontend – Settings page (SETUP-5, SETUP-6)

Route `/settings` with sections:
- **Accounts**: list, add, rename, archive, delete (if no lines), reorder.
- **Subcategories**: list grouped by category; add/edit (name, color picker, hotkey, sort order), move between categories, delete-with-merge.
- **Display**: locale selector (`da-DK`, `en-US`).
- **Database**: Download database button, Replace database (file upload + confirm), list backups with restore buttons.

**Done when:** Vitest component tests for the account/subcategory forms; manual check that download produces a valid SQLite file.

### 7. Frontend – Import page (IMP)

Route `/import`:
- Select account (dropdown) + file picker → `POST /api/imports/preview`.
- **Preview screen** (IMP-5): date range, new lines count, duplicates count, parse errors (with row numbers), warnings (balance gaps, account mismatch, duplicate SHA). Confirm / Cancel buttons.
- On confirm → `POST /api/imports` → **Result screen** (IMP-12): lines imported, duplicates skipped, uncategorized count, button "Categorize now" (links to `/categorize` – placeholder for M2) and "Import another".
- **Import history** section (IMP-14): table of all imports with account, file name, date range, counts, status (active/undone), and an **Undo** button on the latest non-undone import (IMP-13).

**Done when:** Playwright e2e: upload fixture CSV → preview → confirm → see result → see history → undo → lines gone.

### 8. Frontend – Statements view (STMT)

Route `/statements`:
- Table with columns: date, account, message, amount, balance, category, subcategory, source, spread (STMT-1). In M1 all lines have source=`none` and no subcategory, but columns are present.
- Filter bar: date range (with presets: all, last 12 months, YTD, previous year, custom), accounts (multi-select), direction (in/out/both), amount range, message text search, categories, subcategories, source, import batch, spread > 1 (STMT-2).
- Server-side sorting (click column headers) and pagination (50/page) (STMT-3).
- Totals bar: sum in, sum out, net, count for the filtered set (STMT-4).
- Filter state in URL query params; back/forward works (STMT-5).
- "Categorize these" button → `/categorize?...same filters` (STMT-6, placeholder for M2).

**Done when:** Playwright e2e: import fixture → go to statements → filter by account → verify totals match → paginate → URL reflects filter.

### 9. i18n

Add all new UI strings to `web/messages/en.json` under namespaces: `welcome.*`, `settings.*`, `import.*`, `statements.*`, `accounts.*`, `subcategories.*`.

### 10. Tests

**Go unit:**
- `csvparse`: all edge cases (see step 2).
- `imports`: preview dedup, balance continuity gaps, account sanity mismatch, SHA-256 duplicate warning, commit + undo round-trip.
- `db`: migration 00002 applies cleanly, integrity + FK checks, fixture DBs (none yet for M1).

**Go integration (httptest + temp dir):**
- All new API endpoints: accounts CRUD, subcategories CRUD + merge, import preview/commit/undo/history, lines filtering/pagination/totals, database upload/download/replace, backups list/restore.

**Frontend:**
- Vitest: import preview component rendering, statements filter state → URL serialization.
- Playwright e2e: welcome wizard → import → statements → undo flow.

**Fixtures:** add anonymized sample CSV files to `testdata/fixtures/` covering: normal file, file with BOM + `;` in message, file with parse errors, file with non-DKK rows, overlapping exports (for dedup), file with balance gaps. Since 2026-11-05 these must be in the v2 16-column format — see [csv-format-v2.md](csv-format-v2.md) step 5.

## Definition of done (M1)

- [ ] Fresh start shows welcome; create-new wizard collects accounts + subcategories and lands on import.
- [ ] Upload existing DB works; older schema is migrated; newer is refused.
- [ ] Import CSV → preview shows counts/errors/warnings → confirm → result screen → history lists it.
- [ ] Re-importing an overlapping file skips duplicates.
- [ ] Undo import removes the lines and marks the batch undone.
- [ ] Statements view shows all lines with filters, sorting, pagination, totals; filter state in URL.
- [ ] Settings: manage accounts + subcategories, download/replace DB, backups.
- [ ] `task test`, `task lint`, `task check:generated` all pass.
- [ ] Playwright e2e for the core flow passes.

## Out of scope for M1

Rules (M3), transfers (M4), categorization page (M2), charts (M5), undo action log for non-import actions (M2), spread UI (M2), action history panel (M6).

## Risks

| Risk | Mitigation |
|---|---|
| Preview→commit tampering (user edits preview payload) | Server re-validates: re-parse the uploaded file on commit and compare SHA-256 with the preview token. |
| Large CSV upload memory | Stream-parse with `bufio.Scanner` (increase buffer to 1 MiB/line); don't hold the whole file in memory beyond hashing. |
| Balance continuity false positives on first import of an account | Only warn, never block; skip check when the account has no existing lines. |
| sqlc generated code drift | `task check:generated` in CI; regenerate after every migration. |
