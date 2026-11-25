# M5 – Visualization: implementation plan

Reference: [PRD.md](../../PRD.md) section 11, milestone M5, and section 7.9 (VIZ).

## Goal

A dashboard at `/visualize` with the six default charts from PRD VIZ-1, powered by Apache ECharts, with global filters (VIZ-2), per-chart options persisted in `setting` (VIZ-3), click-through drill-down to the statements view (VIZ-4), URL-encoded filter state (VIZ-5), category/subcategory colors (VIZ-6), and spread-aware aggregation (PRD §7.9 metric definitions). Lines with `source = 'transfer'` are excluded by default (toggleable). Uncategorized and conflict lines appear as their own series so gaps are visible.

Requirements covered (from PRD section 7): VIZ-1..7. (VIZ-8 saved dashboards is Later.)

## Decisions

| # | Decision |
|---|---|
| D1 | **Aggregation happens in Go, not SQL.** A new package `internal/viz` fetches the filtered line set (reusing the same filter predicate as `GET /api/lines`, see D3) and aggregates in memory: group by month (or quarter/year), then by category/subcategory, applying spread (D5). At ≤40k lines this is fast (<200 ms, NFR-3) and keeps the spread logic in one tested place. SQL stays simple: one `SELECT` with the shared WHERE clause, no GROUP BY. |
| D2 | **One aggregation endpoint per chart family**, not per chart: `GET /api/viz/timeseries` returns monthly/quarterly/yearly buckets with per-category and per-subcategory sums (income, fixed, variable, savings, uncategorized, conflict), plus per-account sums. The frontend derives all six charts (VIZ-1a–f) from this one payload, applying granularity and averaging client-side. This avoids N endpoints and lets the frontend re-bucket without a round trip. A second endpoint `GET /api/viz/share` returns the subcategory share for the selected period (donut/treemap, VIZ-1d). |
| D3 | **Shared filter predicate.** The viz filter params (`from`, `to`, `accounts`, `categories`, `subcategories`, `includeTransfers`) map onto the same `linesWhere` SQL used by `GET /api/lines` (STMT-2). The viz package takes a `vizFilters` struct, builds the WHERE clause + args (same pattern as `lineFilters.filterArgs`), and runs the query. `includeTransfers=false` adds `AND sl.source != 'transfer'` (VIZ default). No new migration needed — the filter reuses existing columns. |
| D4 | **Granularity and averaging are client-side.** The backend always returns monthly buckets (`YYYY-MM`); the frontend re-buckets to quarter (`YYYY-Qn`) or year (`YYYY`) and computes rolling X-month averages (VIZ-1e, VIZ-2 averaging window). This keeps the backend payload stable and makes the averaging window a pure UI control. |
| D5 | **Spread (PRD §7.9):** a line with `spread_months = X` contributes `amount / X` to its booking month and each of the next X−1 months. Remainder (øre) goes to the first month. Implementation: in the Go aggregator, for each line with `spread_months > 1`, compute `base = amount / X` (integer division, signed), `rem = amount - base*X`, and add `base+rem` to the first month, `base` to the rest. Months are computed from `booking_date` by adding calendar months (not 30-day offsets). Spread is capped at 36 (schema CHECK, VIZ-7 already enforced). |
| D6 | **Net sums per category (PRD §7.9):** expense categories (fixed, variable, savings) are summed as-is (negative amounts) and displayed as positive magnitudes in charts. Income is positive. Uncategorized (`source='none'`) and conflict (`source='conflict'`) lines are aggregated into their own pseudo-series (grey/neutral colors) so they are always visible. Transfer lines are excluded unless `includeTransfers` is on; when included, they are shown as a separate grey series (not folded into any category). |
| D7 | **ECharts is a new frontend dependency** (`echarts` npm package, bundled — no CDN, NFR-1). Charts render client-side only (CSR), so no SSR concerns. Each chart is a Svelte component wrapping an ECharts instance with resize handling. Colors come from the category/subcategory `color` column (VIZ-6, CAT-5), fetched via the existing `GET /api/categories` + `GET /api/subcategories`. |
| D8 | **Per-chart config persisted in `setting` (VIZ-3).** Keys like `viz.chartType.<chartId>`, `viz.stacked.<chartId>`, `viz.visibleSeries.<chartId>`. The dashboard page loads settings on mount and saves on change (debounced) via the existing `PUT /api/settings`. Default config is defined in `web/src/lib/defaults.ts`. |
| D9 | **Drill-down (VIZ-4):** clicking a chart segment navigates to `/statements?from=...&to=...&categories=...&subcategories=...&source=...` with the exact filter matching the clicked bucket (month range, category/subcategory, source). The statements page already supports all these params (STMT-5/6), so no backend change is needed — the frontend just constructs the URL. |
| D10 | **Filter state in URL (VIZ-5):** the visualize page encodes its filter state (`from`, `to`, `accounts`, `categories`, `subcategories`, `granularity`, `includeTransfers`, `avgWindow`) in the URL query string, reusing the `URLSearchParams` pattern from `statementFilters.ts`. Back/forward works via SvelteKit's `$page` store. |
| D11 | **No migration needed for M5.** The `setting` table already exists; `spread_months` already exists on `statement_line`; all filter columns exist. M5 adds only Go code (`internal/viz`, server handlers), sqlc queries for the line fetch, and frontend code. If a viz-specific setting is ever needed, it goes in `setting` — no schema change. |

## Current state (from M4)

- `/visualize` route exists as a placeholder (`web/src/routes/visualize/+page.svelte` shows "placeholder.m5"); the nav item is `disabled: true` in `+layout.svelte`.
- `GET /api/lines` has the full filter predicate (`linesWhere` in `internal/server/lines.go`) with `from`, `to`, `accounts`, `direction`, `amountMin/Max`, `message`, `categories`, `subcategories`, `source`, `importId`, `spreadGt` — the viz filters are a subset plus `includeTransfers`.
- `statement_line` has `spread_months` (1–36), `source` (incl. `transfer`, `conflict`), `subcategory_id`, `booking_date`, `amount_minor`.
- `category` (4 rows, seeded) and `subcategory` have `color` columns; `GET /api/categories` and `GET /api/subcategories` return them.
- `setting` table + `GET/PUT /api/settings` exist (used for locale).
- `web/src/lib/statementFilters.ts` has `StatementFilters`, `presetToRange`, `rangeToPreset` — reusable for the viz date presets (VIZ-2).
- `web/src/lib/api.ts` is the API client; `apiFetch<T>` wrapper exists.
- ECharts is **not** yet in `package.json`.
- No `internal/viz` package; no viz API endpoints.

## Steps

### 1. Add ECharts dependency + sqlc query for viz line fetch

- `cd web && npm install echarts` (adds to `package.json` dependencies; bundled by Vite, no CDN).
- New sqlc query `internal/db/queries/list_lines_for_viz.sql`: same SELECT as `linesSelect` but without pagination (LIMIT/OFFSET handled in Go caller) and without ORDER BY (aggregation doesn't need order). Columns: `id, account_id, booking_date, amount_minor, subcategory_id, source, spread_months, category_id` (via JOIN subcategory→category). WHERE clause is built dynamically like `linesWhere` but with the viz param set (D3).

**Done when:** `npm ls echarts` shows the package; `task generate` produces clean sqlc output for the new query.

### 2. Viz aggregation package `internal/viz`

- `type Filters struct { From, To string; AccountIDs, CategoryIDs, SubcategoryIDs []int64; IncludeTransfers bool }`
- `type MonthBucket struct { Month string /* YYYY-MM */; ByCategory map[string]int64 /* category key → net sum minor */; BySubcategory map[int64]int64; ByAccount map[int64]int64; Uncategorized int64; Conflict int64; Transfer int64 }`
- `FetchLines(ctx, q, f) ([]VizLine, error)` — runs the sqlc query with the dynamic WHERE (D3).
- `Aggregate(lines []VizLine, f Filters) []MonthBucket` — applies spread (D5), groups by month, then by category key (via subcategory→category lookup, passed in) and subcategory ID; computes uncategorized/conflict/transfer buckets (D6).
- `Rebucket(buckets []MonthBucket, granularity string) []Bucket` — client-side helper? No — rebucketing is frontend (D4), so this stays frontend. Backend returns monthly only.
- `Share(lines []VizLine, f Filters) []SubcategoryShare` — for the donut/treemap (VIZ-1d): sum per subcategory within the selected period (respecting spread? No — share chart uses raw amounts in the period, spread only affects time series. PRD §7.9 says spread applies to charts generally, but for a share-of-period donut, spreading would double-count across months outside the range. **Decision:** the share chart uses raw (unspread) amounts for lines whose booking month is within the selected range. This is the intuitive "where did my money go in this period" view.)
- Unit tests: spread remainder to first month; spread across year boundary (Dec + 3 months → Dec, Jan, Feb, Mar); quarter rebucket (frontend, but test the Go monthly aggregation); net sums per category; transfer exclusion default; uncategorized/conflict as own series; empty result; 40k-line benchmark (<200 ms).

**Done when:** `go test ./internal/viz/...` passes, including the performance benchmark.

### 3. HTTP API endpoints

| Method | Route | Description |
|---|---|---|
| `GET` | `/api/viz/timeseries` | Query params: `from`, `to`, `accounts`, `categories`, `subcategories`, `includeTransfers`. Returns `{ buckets: [{ month, byCategory: {income, fixed, variable, savings}, bySubcategory: {id: sum}, byAccount: {id: sum}, uncategorized, conflict, transfer }] }`. |
| `GET` | `/api/viz/share` | Same params. Returns `{ shares: [{ subcategoryId, subcategoryName, categoryKey, color, amountMinor }] }` for the donut/treemap (VIZ-1d). Uses raw amounts in range (D2 note). |

Both handlers live in `internal/server/viz.go`, registered in `server.go` `setupRoutes`. They parse params (reusing `parseIDList` from `lines.go`), call `viz.FetchLines` + `viz.Aggregate`, and return JSON.

**Done when:** Go integration tests (httptest + temp SQLite) pass: timeseries returns correct monthly buckets with spread applied; share returns correct subcategory sums; filters work (date range, accounts, categories, includeTransfers toggle); empty DB returns empty arrays (not null).

### 4. Frontend – chart components

New files under `web/src/lib/viz/`:

- `Chart.svelte` — generic ECharts wrapper: takes `options` prop, creates chart on mount, `setOption` on change, resize observer, cleanup on destroy.
- `MonthlyStackedBar.svelte` — VIZ-1a: stacked bars of fixed/variable/savings per month, income as a line (combo chart).
- `IncomeVsExpenses.svelte` — VIZ-1b: income vs expenses vs savings per month (grouped bars) + savings rate line (secondary axis).
- `SubcategoryBreakdown.svelte` — VIZ-1c: stacked bars of subcategories for a selected category over time; category selector dropdown.
- `SubcategoryShare.svelte` — VIZ-1d: donut (or treemap) of subcategory share for the selected period.
- `RollingAverage.svelte` — VIZ-1e: line chart of rolling X-month average per category/subcategory; X from the averaging window control.
- `AccountAverage.svelte` — VIZ-1f: bar chart of per-account average monthly spending over the last X months.

Each component receives the aggregated data + colors as props and emits a `drilldown` event with the filter params (D9).

**Done when:** Vitest component tests pass for each chart (renders with mock data, drilldown event fires with correct params); `task test:web` green.

### 5. Frontend – visualize page (`/visualize`)

Rewrite `web/src/routes/visualize/+page.svelte`:

- **Filter bar** (top): date range preset dropdown (reuse `presetToRange` from `statementFilters.ts`: last 12 months, YTD, previous year, all, custom) + custom date inputs; account multi-select; category multi-select; subcategory multi-select; granularity select (month/quarter/year); include-transfers checkbox; averaging window number input (VIZ-2).
- **Dashboard grid**: the six charts in a responsive grid (2 columns on desktop, 1 on mobile). Each chart card has a header with title + per-chart options (VIZ-3): chart type toggle where meaningful, stacked/grouped toggle, series visibility checkboxes.
- **State management**: filter state in URL (D10, VIZ-5) via `$page.url.searchParams`; per-chart config in `setting` (D8) loaded on mount, saved debounced on change.
- **Data fetching**: on filter change, call `GET /api/viz/timeseries` and `GET /api/viz/share` with the current params; re-bucket and compute averages client-side (D4); pass to chart components.
- **Drill-down**: chart click → `goto('/statements?' + params)` (D9).

Enable the nav item in `+layout.svelte` (remove `disabled: true`).

**Done when:** `task test:web` passes; manual check: all six charts render with fixture data; filters update the charts; drill-down navigates to statements with correct params; URL updates on filter change; refresh preserves state.

### 6. Frontend – client-side rebucketing + averaging lib

- `web/src/lib/viz/bucket.ts`: `rebucket(monthly: MonthBucket[], granularity: 'month'|'quarter'|'year'): Bucket[]` — merges monthly buckets into quarter/year buckets.
- `web/src/lib/viz/average.ts`: `rollingAverage(buckets: Bucket[], window: number): Bucket[]` — computes rolling X-month average per series (VIZ-1e, VIZ-2 averaging window).
- `web/src/lib/viz/colors.ts`: maps category/subcategory IDs to colors from `GET /api/categories`/`GET /api/subcategories` (VIZ-6); fallback palette for uncategorized (neutral grey) and conflict (warning color).
- Unit tests (Vitest) for rebucket (quarter/year boundaries), rolling average (window edges, partial windows), color mapping.

**Done when:** `task test:web` passes with the new unit tests.

### 7. i18n

Add `viz.*` namespace to `web/messages/en.json`: chart titles, filter labels, option labels, empty-state messages, drill-down tooltip. Add `nav.visualize` (already exists). Remove `placeholder.m5` usage.

### 8. Tests

**Go unit (`internal/viz`):**
- Spread: remainder to first month, across year boundary, spread=1 (no-op), spread=36.
- Aggregation: net sums per category, uncategorized/conflict as own series, transfer excluded by default / included when flag set, empty input.
- Share: raw amounts in range, excluded categories.
- Benchmark: 40k lines aggregate <200 ms (NFR-3).

**Go integration (httptest + temp SQLite):**
- `GET /api/viz/timeseries` with fixture data: correct buckets, spread applied, filters (date, accounts, categories, includeTransfers), empty DB → `[]`.
- `GET /api/viz/share`: correct shares, filters, empty → `[]`.

**Frontend (Vitest):**
- `bucket.ts`: rebucket quarter/year boundaries.
- `average.ts`: rolling average window edges.
- `colors.ts`: color mapping.
- Chart components render with mock data + drilldown event.

**Playwright e2e** (against native binary with temp `FA_DATA_DIR` + fixture CSVs imported):
1. Setup → import fixture CSVs → categorize a few lines via rules → go to `/visualize`.
2. All six charts render with data.
3. Change date preset to "last 12 months" → charts update; URL updates; refresh → same state.
4. Toggle include-transfers → transfer series appears/disappears.
5. Change granularity to quarter → time series re-buckets.
6. Set averaging window to 3 → rolling average chart shows 3-month averages.
7. Click a bar in the monthly stacked chart → navigates to `/statements` with correct `from`/`to`/`categories` params → statements filtered accordingly.
8. Change a per-chart option (e.g. stacked → grouped) → persists across reload (setting saved).
9. Set spread on a line via categorize page → visualize → the line's amount is distributed across months in the time series.

**Done when:** `task test`, `task lint`, `task check:generated` pass and the Playwright flow is green.

## Definition of done (M5)

- [ ] ECharts installed and bundled; no CDN (NFR-1).
- [ ] `GET /api/viz/timeseries` and `GET /api/viz/share` return correct aggregated data with spread applied (VIZ-1, §7.9), transfer excluded by default (toggleable), uncategorized/conflict as own series.
- [ ] All six default charts render on `/visualize` (VIZ-1a–f) with category/subcategory colors (VIZ-6).
- [ ] Global filters work: date range presets + custom, accounts, categories, subcategories, granularity, include transfers, averaging window (VIZ-2).
- [ ] Per-chart options (chart type, stacked/grouped, series visibility) persist in `setting` (VIZ-3).
- [ ] Clicking a chart segment navigates to `/statements` with the matching filter (VIZ-4).
- [ ] Filter state is in the URL; back/forward and refresh work (VIZ-5).
- [ ] Spread (`spread_months`) is reflected in time-series charts (PRD §7.9, VIZ-7 already settable from categorize page).
- [ ] Nav item for Visualize is enabled; placeholder removed.
- [ ] `task test`, `task lint`, `task check:generated` and the Playwright e2e flow pass; 40k-line aggregation <200 ms.

## Out of scope for M5

- Saved dashboards / named views (VIZ-8, Later).
- Categorization suggestions (Later).
- Other rule types (RULE-10, Later).
- Action history panel UI, shortcut overlay, i18n extraction, performance pass (M6).
- Other bank formats (IMP-15, Later).

## Risks

| Risk | Mitigation |
|---|---|
| 40k-line fetch + aggregation too slow | Fetch only needed columns (no message text); benchmark in unit tests; if needed, add a SQL-side pre-aggregation later (but start with Go, D1). Partial index on `booking_date` already exists implicitly via the transfer scan index. |
| Spread across year boundaries is fiddly | Calendar-month arithmetic in Go (`time.AddDate(0, n, 0)`); unit tests cover Dec→Mar, leap years. |
| ECharts bundle size bloats the SPA | Use `echarts/core` with only the needed charts/components (tree-shaking via `echarts/core` + `use()`), not the full `echarts` package. Measure bundle size; keep under ~300 kB gzipped for the chart chunk. |
| Share chart with spread double-counts | Share chart uses raw amounts for lines booked in range (D2 note); spread only affects time series. Documented in code comment. |
| URL state gets too long with many subcategories | Subcategory IDs are comma-separated ints — compact enough for dozens of subcategories. If it grows unwieldy, switch to a compressed encoding later. |
| Chart click drill-down params don't match statements filter exactly | Reuse the exact param names from `statementFilters.ts` (`from`, `to`, `categories`, `subcategories`, `source`); integration test verifies the statements page filters correctly after drill-down. |
