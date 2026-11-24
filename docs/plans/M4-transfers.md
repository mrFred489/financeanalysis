# M4 – Transfers: implementation plan

Reference: [PRD.md](../../PRD.md) section 11, milestone M4.

## Goal

Automatic and manual detection of internal transfers between the user's own accounts: candidate matching (different accounts, same booking date, equal and opposite amounts), auto-linking of unique 1:1 pairs, a **Transfer review** view for ambiguous candidates, manual link/unlink, manual recategorization of one side, and statements/categorize UI showing the link with a jump to the counterpart. Transfer detection runs after every import; all link/unlink actions are undoable via the M2 action log.

Requirements covered (from PRD section 7): TRF-1..9, IMP-11 (transfer detection on import), IMP-12 (result screen counts), IMP-13 (import undo removes transfer links), UNDO-1 (transfer link/unlink), CATZ-7 (jump-to-counterpart slot), STMT-1/STMT-9 (transfer column + link).

## Decisions

| # | Decision |
|---|---|
| D1 | New tables come via migration `00006_m4_transfers.sql`: `transfer_link(id, out_line_id UNIQUE, in_line_id UNIQUE, method, created_at)` and `transfer_rejection(out_line_id, in_line_id, PRIMARY KEY (out_line_id, in_line_id))` (PRD section 6). `transfer_link` uses `ON DELETE CASCADE` on both line references so import undo (delete lines) cleans up links automatically. |
| D2 | Transfer detection lives in a dedicated package `internal/transfers`: `Detect(ctx, q)` scans all unlinked, non-rejected candidate lines and returns pairs to auto-link plus ambiguous groups. Detection is pure Go over a SQL-fetched candidate set (indexed by `(account_id, booking_date, amount_minor)`), not complex SQL — same pattern as the M3 rules engine (plan M3 D2). |
| D3 | Auto-link rule (TRF-2): a candidate pair is linked automatically only when it is a **unique 1:1** — the out-line has exactly one candidate in-line, and that in-line has exactly one candidate out-line. Ambiguous pairs (TRF-3) are never auto-linked; they are listed in the Transfer review view. |
| D4 | Detection runs over **all** data after every import commit (TRF-4), because the counterpart account's file may be imported later. It also runs on demand via `POST /api/transfers/detect`. Detection is **not** recorded in the action log (it is derived state, like rule evaluation on import); only explicit user link/unlink actions are undoable (UNDO-1). |
| D5 | Linked lines get `source='transfer'`, `subcategory_id=NULL`, `rule_id=NULL` (TRF-5). The rules engine already skips `source='transfer'` lines (M3), so no rules change is needed. Unlinking (TRF-6) sets both lines back to `source='none'` (then rules re-evaluate them), records the pair in `transfer_rejection`, and is one undoable action. |
| D6 | Manual link (TRF-7): `POST /api/transfers/link` body `{ "outLineId": n, "inLineId": n }`. Validates: different accounts, `amount_out = -amount_in` (exact opposite), neither already linked. Any date is allowed (TRF-7), unlike auto-detection (same date only, TRF-1). Sets both lines `source='transfer'`. One undoable action `transfer_link`. |
| D7 | Unlink (TRF-6): `POST /api/transfers/{id}/unlink`. Deletes the `transfer_link` row, inserts a `transfer_rejection` row for the pair, sets both lines back to `source='none'` + re-evaluates through rules. One undoable action `transfer_unlink` (snapshots both lines + the link row + the rejection row). |
| D8 | Manual recategorization of one side (TRF-8): the existing `POST /api/lines/{id}/categorize` already sets `source='manual'`; when the line is part of a transfer link, the link **remains** (it is only displayed, not removed). No new endpoint needed; just ensure the categorize endpoint does not reject linked lines and the UI shows the link. |
| D9 | Import integration: `imports.Commit` calls `transfers.DetectAll` after rule evaluation (IMP-11) inside the import transaction; `imports.UndoLatest` relies on `ON DELETE CASCADE` from `statement_line` to remove transfer links (IMP-13). The commit result gains `transferMatched` and `transferAmbiguous` counts (IMP-12). |
| D10 | `transfer_link` and `transfer_rejection` are added to `actions.entityColumns` so undo can snapshot/restore them. `transfer_link` has a single `id` PK, so it is straightforward. `transfer_rejection` has a composite key `(out_line_id, in_line_id)`; encode `entity_id = out_line_id * 100000000 + in_line_id` (line IDs are int64 and far below 10^8 in practice) or, cleaner: store rejection rows as part of the `transfer_link` entity snapshot (the link's `after_json` includes a `rejectedPair` field). **Chosen:** snapshot `transfer_rejection` rows with `entity="transfer_rejection"` and a synthetic `entity_id` decoded in the restore hook (see step 3). |
| D11 | Statements view (STMT-1/9) gains a **transfer** column: shows "→ #<counterpartId>" or "← #<id>" with a link to `/categorize?focus=<counterpartId>` (or `/statements?focus=`). The `GET /api/lines` response is extended with `transferLinkId` and `transferCounterpartId` (nullable), fetched via a LEFT JOIN on `transfer_link`. |
| D12 | Categorize page (CATZ-7): the disabled "Transfer" action button becomes **"Jump to counterpart"** when the focused line is linked; it navigates to `/categorize?focus=<counterpartId>`. A new **"Unlink transfer"** button appears on linked lines (calls unlink endpoint). |

## Current state (from M3)

- `statement_line` has `source` including `'transfer'` in the CHECK constraint (migration 00002/00003), but no code ever sets it yet.
- `counterparty_account` is stored on import for transfer rows (columns 2/3), but it is not used for matching.
- Rules engine (`internal/rules`) skips `source IN ('manual','transfer')` — already correct.
- `actions` engine with `Record`/`Undo`/`Redo`, entities `statement_line`, `rule`, `conflict_resolution`.
- `imports.Commit` runs `rules.EvaluateLines` on new lines; `imports.UndoLatest` deletes lines by import (cascade will handle transfer links once FKs exist).
- `GET /api/lines` returns lines with `source` filter (incl. `transfer`); the categorize page has a disabled "Transfer" button.
- Statements page shows a `source` column; no transfer link column yet.
- No `transfer_link` / `transfer_rejection` tables yet.

## Steps

### 1. Migration `00006_m4_transfers.sql` + sqlc queries

Tables (STRICT):

- `transfer_link(id INTEGER PK, out_line_id INTEGER NOT NULL UNIQUE REFERENCES statement_line(id) ON DELETE CASCADE, in_line_id INTEGER NOT NULL UNIQUE REFERENCES statement_line(id) ON DELETE CASCADE, method TEXT NOT NULL DEFAULT 'auto' CHECK(method IN ('auto','manual')), created_at TEXT NOT NULL DEFAULT (strftime(...)))`
- `transfer_rejection(out_line_id INTEGER NOT NULL, in_line_id INTEGER NOT NULL, created_at TEXT NOT NULL DEFAULT (strftime(...)), PRIMARY KEY (out_line_id, in_line_id))` — no FK (the lines may be deleted; rejection rows for deleted lines are harmless and cleaned on unlink/import-undo).
- Index for candidate detection: `CREATE INDEX idx_statement_line_transfer_scan ON statement_line (booking_date, amount_minor) WHERE source != 'transfer'` — partial index for the detection scan. Also `CREATE INDEX idx_transfer_link_out ON transfer_link(out_line_id)` and `idx_transfer_link_in ON transfer_link(in_line_id)` (the UNIQUE constraints already create these).

sqlc queries to add in `internal/db/queries/`:
- Transfer links: `create_transfer_link.sql`, `get_transfer_link.sql`, `get_transfer_link_by_line.sql` (either side), `list_transfer_links.sql` (all, with line details), `delete_transfer_link.sql`, `list_linked_line_ids.sql` (set of line IDs that are linked, for detection exclusion).
- Transfer rejections: `insert_transfer_rejection.sql`, `list_transfer_rejections.sql`, `transfer_rejection_exists.sql` (check a pair), `delete_transfer_rejection.sql`.
- Candidate scan: `list_transfer_candidates.sql` — all lines with `source != 'transfer'` and not in any rejection pair, ordered by `(booking_date, amount_minor)`; the Go code builds the candidate graph.
- Ambiguous review: `list_ambiguous_transfer_groups.sql` — computed in Go from candidates; the endpoint returns groups.
- Line queries: extend `list_statement_lines.sql` and `get_statement_line.sql` with LEFT JOIN on `transfer_link` to return `transfer_link_id`, `transfer_counterpart_id`. Add `set_line_source_transfer.sql` (sets `source='transfer'`, `subcategory_id=NULL`, `rule_id=NULL`) and `clear_line_transfer.sql` (sets `source='none'`, clears subcategory/rule_id — used on unlink before rule re-evaluation).

**Done when:** `task generate` produces clean sqlc output; migration test passes (empty → latest, integrity + FK checks; fixture DB from M3 migrates cleanly).

### 2. Transfer detection engine `internal/transfers`

- `type Pair struct { OutLineID, InLineID int64 }`
- `type AmbiguousGroup struct { OutLineID int64; CandidateInLineIDs []int64 }` — an out-line with >1 candidates (TRF-3).
- `Candidates(ctx, q) ([]dbq.ListTransferCandidatesRow, error)` — fetch all unlinked lines with `source != 'transfer'`.
- `BuildGraph(lines) map[outKey][]inLine` — group by `(booking_date, -amount_minor)`; an out-line (amount < 0) matches in-lines (amount > 0) with the same booking date and opposite amount, on a **different account**, neither linked, and the pair not in `transfer_rejection` (TRF-1).
- `Detect(ctx, q) (autoPairs []Pair, ambiguous []AmbiguousGroup, err error)` — for each out-line, find candidates; if exactly 1 candidate and that candidate has exactly 1 candidate back (unique 1:1, TRF-2), it is an auto-pair; otherwise the group is ambiguous (TRF-3). Returns both.
- `ApplyAutoLinks(ctx, q, pairs []Pair) ([]actions.Change, error)` — for each auto pair: insert `transfer_link(method='auto')`, set both lines `source='transfer'` (via `set_line_source_transfer`). Returns changes (but per D4 these are **not** recorded in the action log on import; they are returned so the caller can count them).
- `DetectAll(ctx, d *sql.DB) (autoCount, ambiguousCount int, err error)` — convenience wrapper used by import commit and the on-demand endpoint: runs `Detect`, applies auto-links in a transaction, returns counts.
- Unit tests: unique 1:1 auto-linked; ambiguous (2 candidates) not linked; same account excluded; already-linked excluded; rejected pair excluded; different dates excluded; opposite amounts required; zero-amount lines excluded; detection idempotence (running twice does not re-link).

**Done when:** `go test ./internal/transfers/...` passes.

### 3. Actions engine extension

- Add `EntityTransferLink = "transfer_link"` and `EntityTransferRejection = "transfer_rejection"` to `actions.entityColumns` and `entityKey`.
  - `transfer_link` columns: `id, out_line_id, in_line_id, method, created_at`; key = `id`.
  - `transfer_rejection` columns: `out_line_id, in_line_id, created_at`; key is composite. Encode `entity_id = out_line_id * 100_000_000 + in_line_id` and add a custom restore hook in `applySnapshot` that decodes the synthetic key back into the two columns (delete + insert, since there is no single PK column to upsert by).
- Unit tests: undo of `transfer_link` (manual) restores both lines to their previous source and removes the link; undo of `transfer_unlink` re-creates the link, removes the rejection, and restores line sources; round trip across DB close/reopen.

**Done when:** `go test ./internal/actions/...` passes with the new entities.

### 4. HTTP API endpoints

All under `/api/`, same error shape. Mutation endpoints: validate → begin tx → mutate → `actions.Record` → commit.

| Method | Route | Description |
|---|---|---|
| `GET` | `/api/transfers` | List all transfer links with both lines' details (account, date, message, amount) and method. |
| `POST` | `/api/transfers/detect` | Run detection over all data now (TRF-4). Returns `{ autoLinked: n, ambiguous: m }`. Auto-links are applied (not undoable, D4). |
| `GET` | `/api/transfers/review` | Ambiguous candidate groups (TRF-3): each out-line with its candidate in-lines. |
| `POST` | `/api/transfers/link` | Manual link (TRF-7). Body `{ "outLineId": n, "inLineId": n }`. Validates different accounts + opposite amounts + neither linked. Sets both `source='transfer'`. Undoable `transfer_link`. |
| `POST` | `/api/transfers/{id}/unlink` | Unlink (TRF-6). Deletes link, inserts rejection, resets both lines to `none` + re-evaluates through rules. Undoable `transfer_unlink`. |
| `POST` | `/api/transfers/review/dismiss` | Dismiss an ambiguous group: insert rejection pairs for all candidate combinations of that out-line, so they are never auto-matched again (TRF-3). Undoable `transfer_dismiss`. |
| `GET` | `/api/lines/{id}/transfer` | Returns the transfer link + counterpart line for a given line (used by statements/categorize UI). |

Also modify existing endpoints:
- `GET /api/lines` and `GET /api/lines/{id}`: add `transferLinkId` + `transferCounterpartId` to the response (D11).
- `POST /api/lines/{id}/categorize`: allow linked lines (TRF-8); the link stays. No rejection needed.
- `POST /api/imports` (commit): after rule evaluation, run `transfers.DetectAll` (IMP-11, D9); result gains `transferMatched` and `transferAmbiguous` counts (IMP-12).
- `POST /api/imports/{id}/undo`: `ON DELETE CASCADE` on `transfer_link` handles link cleanup; also delete `transfer_rejection` rows referencing deleted lines (or leave them — they are harmless; choose: delete for cleanliness, via a `DELETE FROM transfer_rejection WHERE out_line_id IN (deleted) OR in_line_id IN (deleted)`).

**Done when:** Go integration tests (httptest + temp SQLite) pass: detect auto-links unique pairs, ambiguous not linked, manual link validation (same account → 400, not opposite → 400, already linked → 409), unlink + undo restores, dismiss + undo, import triggers detection, import undo cascades links, lines response includes transfer fields.

### 5. Frontend – Transfer review page (`/transfers`)

New route `web/src/routes/transfers/+page.svelte`, added to the nav in `+layout.svelte`.

- **Linked transfers list:** table of all `transfer_link` rows: date, out-account → in-account, message, amount, method badge (auto/manual), actions: Unlink (with confirm). Each row links to both lines (`/categorize?focus=<id>`).
- **Review section (TRF-3):** ambiguous groups; each shows the out-line and its candidate in-lines as selectable buttons; the user picks the correct pair (calls `POST /api/transfers/link`) or dismisses the group (`POST /api/transfers/review/dismiss`).
- **"Run detection now"** button → `POST /api/transfers/detect` → toast with counts.

**Done when:** Vitest tests pass for the review group rendering and the link/dismiss actions; manual check that detection after import auto-links and review lists ambiguous groups.

### 6. Frontend – Statements view integration (STMT-1/9, D11)

- Add a **Transfer** column to the statements table: shows "→ #id" / "← #id" when `transferLinkId` is set, as a link to `/categorize?focus=<counterpartId>`.
- The `source` column already shows `transfer`.

**Done when:** Playwright can see a linked line's transfer column and click through to the counterpart.

### 7. Frontend – Categorize page integration (CATZ-7, D12)

- Replace the disabled "Transfer" button: when the focused line has `transferCounterpartId`, show **"Jump to counterpart"** (navigates to `/categorize?focus=<counterpartId>`).
- Add **"Unlink transfer"** button when linked: calls `POST /api/transfers/{linkId}/unlink`, then refetches.
- The detail pane shows the counterpart link when present.

**Done when:** Playwright can jump to the counterpart from the categorize page and unlink a pair.

### 8. Import integration (IMP-11/12/13, D9)

- Backend: `imports.Commit` calls `transfers.DetectAll(ctx, tx)` after `rules.EvaluateLines`; adds `TransferMatched` and `TransferAmbiguous` to `CommitResult`.
- `imports.UndoLatest`: delete `transfer_rejection` rows referencing the deleted lines (cleanup).
- Frontend: import result screen (`/import`) adds "Matched as transfers: N" and "Ambiguous transfer candidates: M" lines, with a link to `/transfers`.

**Done when:** Playwright: import two fixture CSVs (one per account) with matching opposite amounts on the same date → result screen shows the transfer count → `/transfers` lists the auto-linked pair → undo import → link is gone.

### 9. i18n

Add `transfers.*` namespace to `web/messages/en.json` (list, review, actions, toasts, nav item). Add `nav.transfers`. Remove the disabled state for the transfer button on the categorize page.

### 10. Tests

**Go unit:**
- `transfers`: unique 1:1 auto-link, ambiguous, same-account excluded, rejected pair excluded, different dates, opposite amounts, zero-amount excluded, idempotence, ApplyAutoLinks sets source='transfer' (step 2).
- `actions`: new entities (`transfer_link`, `transfer_rejection`) undo/redo round trips (step 3).

**Go integration (httptest + temp dir):**
- All endpoints in step 4, including validation errors (400/409), import triggers detection, import undo cascades, dismiss + undo.

**Frontend:**
- Vitest: transfers list rendering, review group select/dismiss, nav item.
- Playwright e2e against the native binary with temp `FA_DATA_DIR`: import two account CSVs with a matching transfer → result shows count → `/transfers` shows the auto-link → categorize page shows "Jump to counterpart" → click it → counterpart focused → unlink from categorize page → both lines back to `none` (rules re-evaluated) → `Ctrl+Z` undoes the unlink → link restored → ambiguous pair (two same-amount same-date lines on the other account) appears in `/transfers` review → pick one → linked manually → dismiss another group → detection again → dismissed group not re-suggested.

**Done when:** `task test`, `task lint`, `task check:generated` pass and the Playwright flow above is green.

## Definition of done (M4)

- [ ] `transfer_link` / `transfer_rejection` tables exist via migration 00006; `task check:generated` passes.
- [ ] Auto-detection links unique 1:1 pairs (TRF-1/2); ambiguous pairs are listed in `/transfers` review (TRF-3); detection runs after every import (TRF-4) and on demand.
- [ ] Linked lines have `source='transfer'` and are excluded from charts-ready queries (TRF-5); rules engine skips them.
- [ ] Manual link (TRF-7) and unlink (TRF-6) work, are undoable via the action log, and unlink records the rejection pair + re-evaluates both lines through rules.
- [ ] Manual recategorization of one side keeps the link (TRF-8).
- [ ] Statements view shows the transfer link with jump-to-counterpart (TRF-9); categorize page has "Jump to counterpart" + "Unlink transfer" (CATZ-7).
- [ ] Import commit runs transfer detection and the result screen shows matched/ambiguous counts (IMP-11/12); import undo removes links (IMP-13).
- [ ] `task test`, `task lint`, `task check:generated` and the Playwright e2e flow pass.

## Out of scope for M4

Visualization/charts (M5); action history panel UI (M6); ±N-day transfer matching (PRD open question 4, Later — same-day only for now); categorization suggestions (Later); other rule types (RULE-10, Later).

## Risks

| Risk | Mitigation |
|---|---|
| Detection scan over 40k lines is slow on every import | Partial index on `(booking_date, amount_minor)`; Go grouping is O(n); benchmark in tests (NFR-3: import <5s incl. detection + rules). |
| Ambiguous groups grow unbounded | Review view paginates; dismiss inserts rejection pairs so they are never re-suggested. |
| Undo of unlink must restore rejection rows with a composite key | Synthetic `entity_id` encoding + custom restore hook (D10); tested in actions unit tests. |
| Import undo must clean up transfer links and rejections | `ON DELETE CASCADE` on `transfer_link` handles links; explicit delete of `transfer_rejection` rows for deleted lines in `UndoLatest`. |
| Auto-linking a wrong pair (false positive) | Only unique 1:1 same-date opposite-amount pairs are auto-linked; everything else goes to review; unlink is undoable and records a rejection so it never re-matches. |
| `counterparty_account` from the CSV could be used for matching but is unreliable | Matching uses account IDs + amounts + dates (TRF-1), not the CSV counterparty text; `counterparty_account` stays as display-only metadata. |
