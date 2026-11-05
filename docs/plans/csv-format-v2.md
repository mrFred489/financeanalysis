# CSV format v2 – implementation plan

Reference: [PRD.md](../../PRD.md) §7.2 (updated 2026-11-05) and §6. Supersedes the CSV format assumed in [M1-data-in.md](M1-data-in.md) steps 2, 3 and 10.

## What changed

The bank's real export is a fixed **16-column**, `;`-separated file with no header row and **no running balance** — not the `date;message;amount;balance;currency` layout originally assumed. Key differences (full column table in PRD §7.2):

- Message in column 1; two account columns (2 = account, 3 = counterparty, set only on internal transfers); amount in column 4; three dates in columns 7/8/9, where **column 8 is the booking date** (the file is sorted newest-first on it); bank reference in column 10; secondary reference (e.g. MobilePay id) in column 11; extra text in column 14.
- No running balance (column 5 empty in this export) → `balance_minor` becomes nullable; balance-based checks (IMP-7, old IMP-8) are skipped/replaced.
- Currency column 6 empty → defaults to `DKK`.
- Internal transfers are visible inside one account's file (both account numbers in columns 2/3) → enables a direct account sanity check via `account.account_number`.

## Impact map

| Area | Files | Change |
|---|---|---|
| Schema | `internal/db/migrations/00003_csv_format_v2.sql` (new) | Rebuild `statement_line`: `balance_minor` nullable; add `transaction_date`, `value_date`, `bank_ref`, `counterparty_account` (all nullable TEXT); recreate the dedup index without `balance_minor` |
| Queries | `internal/db/queries/*.sql`, regen `internal/db/dbq` | `line_exists.sql` (drop balance param), `insert_statement_line.sql`, `list_statement_lines.sql`, `get_statement_line.sql` (new columns); delete `get_latest_line_before.sql` / `get_earliest_line_after.sql` (balance-seam check dropped) |
| Parser | `internal/csvparse/csvparse.go` (+ tests) | Rewrite for the 16-column layout |
| Import logic | `internal/imports/imports.go` (+ tests) | Dedup key without balance; IMP-7 only when balances present; IMP-8 via account number |
| Server | `internal/server/imports.go`, `internal/server/lines.go` (+ `server_test.go`) | Adapt to new structs; preview info note; no route changes |
| Frontend | `web/src/routes/statements/+page.svelte`, `web/src/routes/import/+page.svelte`, `web/messages/en.json` | Balance column shows "—" when NULL; line detail shows `bank_ref`/`counterparty_account`/transaction+value dates; preview shows no-balance note and detected file account number |
| Fixtures | `testdata/fixtures/csv/*` | Replace old-format fixtures with v2 fixtures |

## Steps

### 1. Migration `00003_csv_format_v2.sql`

Fix-forward only — never edit `00002` (it may already be applied to real databases). SQLite table rebuild per `docs/migrations.md` (12-step procedure): disable FKs, create `statement_line_v2` with the new column set, copy data (existing `balance_minor` values are kept), drop the old table, rename, recreate indexes — the dedup index becomes `(account_id, booking_date, amount_minor, message, occurrence)` — run `PRAGMA foreign_key_check`, re-enable FKs. Then `task generate` (sqlc) and `task check:generated`.

**Done when:** migration tests pass (empty → latest; fixture DB at 00002 → latest; `integrity_check` + `foreign_key_check` clean).

### 2. `internal/csvparse` rewrite

- `RawLine`: `Date` (col 8, `YYYY-MM-DD`), `TransactionDate` (col 7, nullable), `ValueDate` (col 9, nullable), `Message` (col 1), `AccountFrom`/`AccountTo` (cols 2/3, trimmed), `AmountMinor` (col 4), `BalanceMinor *int64` (col 5, nil when empty), `Currency` (col 6, `DKK` when empty), `BankRef` (col 10), `ExtraRef` (col 11), `ExtraText` (col 14), `FileRow`, `Seq`, `Occurrence`.
- Parse rules: strip BOM; `;` delimiter; `LazyQuotes`; skip empty rows; require **≥ 16 fields** — surplus fields (an unquoted `;` in the message) are joined back into the message, fewer than 16 is a parse error with row number (IMP-3); dates in cols 7–9 as `dd-mm-yyyy` (empty → NULL); amount in col 4 in Danish format; empty col 5 → NULL balance; col 6 non-DKK → warning + row excluded (IMP-4); `seq` = reverse of file order (IMP-8a); `occurrence` keyed on (booking date, amount, message) — balance no longer part of the key.
- Update the package doc comment to the new format.

**Done when:** `go test ./internal/csvparse/...` passes on the v2 fixtures (step 5).

### 3. `internal/imports` changes

- Dedup (IMP-6): `LineExists` without the balance parameter.
- IMP-7: run the balance-continuity check only when the file carries balances in column 5; otherwise add an info warning "file has no balance column; continuity check skipped".
- IMP-8: load the selected account's `account_number`; if set and it does not appear in column 2 or 3 of any row, add the `account_mismatch` warning "This file may belong to a different account". Also expose the file's detected account number(s) in `PreviewResult` so the UI can show them.
- Commit: insert the new columns (balance NULL when absent). Undo: unchanged.

**Done when:** unit tests pass for dedup, no-balance info note, account-number mismatch, and commit/undo round-trip.

### 4. Server + frontend

- `internal/server`: adapt handlers to the new `RawLine`/sqlc types; no route or API-shape changes beyond the new fields.
- Statements view: balance cell renders "—" when NULL; line detail shows `bank_ref`, `counterparty_account`, transaction/value dates. Import preview: show the no-balance info note and the detected file account number. Add the new strings to `web/messages/en.json`.

**Done when:** integration tests pass; manual preview of the real snippet in the UI shows the expected counts/warnings.

### 5. Fixtures and tests

Replace the old-format fixtures in `testdata/fixtures/csv/` with v2 equivalents:

- `format_v2_basic.csv` — the real snippet (12 rows, including a MobilePay row and a two-account transfer row);
- BOM variant; quoted message containing `,`; unquoted `;` in the message (field overflow); a short row (< 16 fields → parse error); bad date; bad amount; non-DKK in col 6; a row with a balance in col 5; duplicate rows (occurrence 1/2).

Update `csvparse_test.go`, `imports_test.go` (replace the balance-gap and balance-seam tests with the no-balance skip and account-number mismatch tests) and `server_test.go`.

### 6. Verification

- `task test`, `task lint`, `task check:generated` all pass.
- Migration tests per `docs/migrations.md` §7 (empty → latest, 00002 fixture → latest).
- Manual: import the real snippet through the UI → preview → commit → statements view shows the lines (balance "—") → undo removes them.

## Risks

| Risk | Mitigation |
|---|---|
| Column semantics (cols 5/6; names of date cols 7/9) inferred from a headerless snippet | PRD §12 risk 5; verify against a full export with headers when available; cols 5/6 are parsed as optional so surprises surface as warnings, not crashes |
| Unquoted `;` inside the message shifts columns | Surplus-fields-joined-into-message heuristic; the bank quotes messages containing special chars (observed in the snippet) |
| `bank_ref` assumed unique per line but not relied on for dedup | Dedup key stays `(account_id, booking_date, amount_minor, message, occurrence)`; `bank_ref` is stored for traceability only |
| Existing databases with lines imported in the old format | Migration keeps old balance values; the dedup index change is safe because old rows simply have a balance set |

## Definition of done

- [ ] Migration `00003` applies cleanly from empty and from a 00002 fixture DB; integrity + FK checks pass.
- [ ] `csvparse` parses the real snippet exactly (12 rows, correct fields per column); all edge-case fixtures behave per the unit tests.
- [ ] Preview on a v2 file shows date range, new/duplicate counts, the account-number sanity result and the no-balance info note; commit stores the new columns; undo still works.
- [ ] Statements view lists the imported lines (balance "—"), filtering/sorting/pagination unchanged.
- [ ] `task test`, `task lint`, `task check:generated` pass.
