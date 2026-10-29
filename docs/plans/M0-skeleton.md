# M0 – Skeleton: implementation plan

Reference: [PRD.md](../../PRD.md) section 11, milestone M0.

## Goal

A runnable, tested, end-to-end skeleton with no business features:

- One Go binary serves a JSON API and the embedded Svelte SPA.
- A SQLite database is created in the data directory, migrated with goose, and queried via sqlc.
- The same commands work on **Linux and Windows**: native via Taskfile, and via Docker.
- CI runs all checks on both operating systems.
- The migration guide is written.

## Decisions (confirmed)

| # | Decision |
|---|---|
| D1 | The repository is hosted on GitHub; CI uses GitHub Actions. All checks are also runnable locally with `task ci`. |
| D2 | Go module path: `github.com/mrFred489/financeanalysis`. |
| D3 | Tailwind CSS v4 is used for styling. |
| D4 | Default port 8080. The Vite dev server runs on 5173. |
| D5 | Each milestone creates only the tables it needs. M0 creates `setting` and `category`; later milestones add theirs via new migrations. |

## Prerequisites (developer machine)

- Go (current stable, ≥ 1.24 for `tool` directives in `go.mod`)
- Node.js LTS + npm
- Docker (only for the Docker tasks)
- Task (`go-task`). Also available without installation as `go tool task`, see step 2.

## Target repository layout

```
/
├─ PRD.md
├─ README.md                    # short: prerequisites + how to run
├─ Taskfile.yml
├─ Dockerfile
├─ compose.yaml
├─ sqlc.yaml
├─ go.mod / go.sum
├─ .gitignore / .gitattributes / .editorconfig / .dockerignore
├─ .github/workflows/ci.yml
├─ cmd/financeanalysis/main.go  # entrypoint, flags, subcommands (serve, healthcheck)
├─ internal/
│  ├─ config/                   # env parsing
│  ├─ db/
│  │  ├─ db.go                  # Open + pragmas
│  │  ├─ migrate.go             # goose provider, version checks
│  │  ├─ backup.go              # VACUUM INTO + retention
│  │  ├─ migrations/00001_init.sql
│  │  ├─ queries/*.sql          # sqlc input
│  │  └─ dbq/                   # sqlc output (generated, committed)
│  ├─ server/                   # router, middleware, handlers, SPA handler
│  └─ webui/
│     ├─ embed.go               # //go:build embedui  -> embeds dist/
│     ├─ noembed.go             # //go:build !embedui -> no UI, API only
│     └─ dist/                  # SvelteKit build output (git-ignored)
├─ web/                         # SvelteKit project
│  ├─ src/routes/…
│  ├─ src/lib/api.ts
│  ├─ messages/en.json          # i18n catalog
│  ├─ e2e/                      # Playwright
│  └─ svelte.config.js / vite.config.ts / playwright.config.ts
├─ docs/
│  ├─ migrations.md
│  └─ plans/
└─ testdata/
   └─ fixtures/db/              # fixture DBs from previous releases (empty in M0)
```

**Why `internal/webui/dist` and build tags:**
- `go:embed` cannot reference parent directories, so the SPA build output is written straight into the Go package.
- Keeping the Go package out of `web/` stops `go build ./...` from wandering into `node_modules`.
- With the `embedui` build tag, Go tests compile without a frontend build. Release builds and e2e tests use `-tags embedui`.

## Steps

### 1. Repository bootstrap

- `git init`, default branch `main`.
- `.gitignore`: `data/`, `internal/webui/dist/`, `web/node_modules/`, `web/.svelte-kit/`, `bin/`, Playwright reports, `*.sqlite` outside `testdata/`.
- `.gitattributes`:
  - `* text=auto eol=lf`
  - `*.csv -text` and `*.sqlite binary`, so bank fixtures keep their exact bytes (BOM, line endings) on Windows.
- `.editorconfig`: LF, UTF-8, tabs for Go, 2 spaces for web files.
- `README.md`: prerequisites, `task setup`, `task dev`, `docker compose up`.

**Done when:** the repo is clean on both OSes with no line-ending churn.

### 2. Go module and tools

- `go mod init`.
- Runtime dependencies: `modernc.org/sqlite`, `github.com/pressly/goose/v3`.
- Tool dependencies via `go get -tool` (pinned in `go.mod`, identical on every machine):
  - `github.com/sqlc-dev/sqlc/cmd/sqlc`
  - `github.com/go-task/task/v3/cmd/task`
  - `honnef.co/go/tools/cmd/staticcheck`
- **Verify early:** that `go tool sqlc` builds with `CGO_ENABLED=0` on Windows (sqlc's PostgreSQL parser uses cgo). Fallback: official sqlc release binary or the `sqlc/sqlc` Docker image, called from Taskfile.

**Done when:** `go tool task --version` and `go tool sqlc version` work on Windows and Linux.

### 3. Entrypoint, config, logging, shutdown

- `internal/config`, from environment variables with defaults:

  | Variable | Native default | Docker value |
  |---|---|---|
  | `FA_DATA_DIR` | `./data` | `/data` |
  | `FA_ADDR` | `127.0.0.1:8080` | `0.0.0.0:8080` (host publishes on 127.0.0.1 only) |
  | `FA_LOG_LEVEL` | `info` | `info` |

- `main.go`:
  - Subcommand `serve` (default): load config, create the data dir, open the DB if it exists, start the server.
  - Subcommand `healthcheck`: GET `/api/status`, exit 0/1. Used by the Docker HEALTHCHECK, since distroless has no curl.
  - `log/slog` with JSON or text handler.
  - Graceful shutdown on SIGINT/SIGTERM: `http.Server.Shutdown`, then close the DB.
  - Version injected via `-ldflags "-X main.version=…"`.

**Done when:** `go run ./cmd/financeanalysis` starts, logs its config, and stops cleanly on Ctrl+C.

### 4. Database layer

**`db.Open(ctx, path)`**
- DSN pragmas: `foreign_keys(1)`, `journal_mode(DELETE)`, `synchronous(FULL)`, `busy_timeout(5000)`; `_txlock=immediate`.
- `SetMaxOpenConns(1)`: a single user and a single file means no `SQLITE_BUSY` surprises.

**`db.Migrate(ctx, db, backupDir)`**, using `goose.NewProvider` with the embedded `migrations/` FS:
- Current version > highest known migration → return `ErrSchemaTooNew` (the PRD requires refusing to start).
- Pending migrations on an existing DB (version > 0) → take a backup first (`pre-migrate`), then apply.
- After migrating, run `PRAGMA integrity_check` and `PRAGMA foreign_key_check`; fail on problems.

**`db.Backup(ctx, db, dir, reason)`**
- `VACUUM INTO 'backups/finance-<UTC timestamp>-<reason>.sqlite'`.
- Retention: keep the newest 20.
- Also used later by Download database, import and import undo.

**`00001_init.sql`**
- `setting(key TEXT PRIMARY KEY, value TEXT NOT NULL)`.
- `category(id INTEGER PRIMARY KEY, key TEXT UNIQUE NOT NULL, name TEXT NOT NULL, color TEXT NOT NULL)`.
- Seed: `income`, `fixed`, `variable`, `savings` with base colors.
- Uses `STRICT` tables.

**sqlc**
- `sqlc.yaml`: engine `sqlite`, schema = `internal/db/migrations` (sqlc understands goose annotations), queries = `internal/db/queries`, output package `dbq`.
- Queries: `ListCategories`, `GetSetting`, `UpsertSetting`.
- Generated code is committed; CI checks it is current with `sqlc diff`.

**Done when:** the DB tests listed in the Tests section pass.

### 5. HTTP server

Router: stdlib `http.ServeMux` with method patterns.

| Route | Behavior |
|---|---|
| `GET /api/status` | `{ version, database: "missing" \| "ready", schemaVersion }` |
| `POST /api/database` | Creates and migrates a new DB. `201`, or `409` if one already exists. Minimal stand-in for the M1 welcome wizard; proves the full stack. |
| `/api/*` (other) | JSON `404`. |
| `/*` | SPA handler: serve the static file if it exists, otherwise `index.html` (client-side routing). Long-cache headers for hashed `/_app/immutable/*` assets, `no-cache` for `index.html`. |

Middleware:
- Panic recovery and request logging.
- `http.MaxBytesReader`: 1 MiB default; larger limits only for upload routes later.
- Security headers: `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, and a **Content-Security-Policy restricted to `'self'`** (`default-src 'self'; connect-src 'self'; img-src 'self' data:`). This enforces the PRD's no-external-traffic rule in the browser. Use SvelteKit's `kit.csp` hash mode for its bootstrap script; verify it works for the SPA fallback page, otherwise allow `script-src 'self' 'unsafe-inline'` only.

Other:
- JSON helpers: `writeJSON`, `writeError` with a consistent `{ "error": { "code", "message" } }` shape.
- Server state holds the DB handle behind a mutex, so it can be created at runtime (and replaced in M1).

**Done when:** the server tests pass and `curl /api/status` works natively.

### 6. Frontend scaffold

- SvelteKit 2 + Svelte 5 + TypeScript.
  - `adapter-static` with `fallback: 'index.html'`, output to `../internal/webui/dist`.
  - Root `+layout.ts`: `export const ssr = false;`
- Tailwind CSS v4 and a system font stack; no web fonts to fetch.
- i18n: Paraglide JS with `messages/en.json`. All UI strings go through it from day one.
- `src/lib/api.ts`: a small typed `fetch` wrapper that parses the error shape. Types are hand-written to match the Go structs (generation, e.g. `tygo`, can be added later if drift becomes a problem).
- `vite.config.ts`: dev proxy `/api` → `http://127.0.0.1:8080`.
- Pages:
  - App shell with navigation placeholders: Statements, Categorize, Rules, Visualize, Import, Settings.
  - `/`: calls `/api/status`. If `missing`, shows a **Create database** button; if `ready`, shows the schema version.
- Tooling: ESLint, Prettier (with the Svelte plugin), `svelte-check`, Vitest.

**Done when:** `npm run build` writes to `internal/webui/dist`, and `npm run check`, `npm run lint` and `npm test` pass.

### 7. Taskfile (single cross-platform entry point)

Use `{{exeExt}}` for binary names and Task's built-in cross-platform commands; no `rm -rf` or bash-isms.

| Task | Does |
|---|---|
| `setup` | `go mod download`, `npm ci` in `web/`, `npx playwright install chromium` |
| `dev` | Runs `dev:api` (`go run`, `FA_DATA_DIR=./data`) and `dev:web` (Vite) in parallel |
| `generate` | `sqlc generate` |
| `build:web` | Frontend production build into `internal/webui/dist` |
| `build` | `build:web`, then `CGO_ENABLED=0 go build -tags embedui -trimpath` → `bin/financeanalysis{{exeExt}}` |
| `test` | `test:go` (`go test ./...`) + `test:web` (Vitest) |
| `e2e` | `build`, then Playwright against the binary |
| `lint` | `go vet`, `staticcheck`, `gofmt -l` check, ESLint, Prettier check, `svelte-check` |
| `check:generated` | `sqlc diff` |
| `ci` | lint + check:generated + test + e2e |
| `docker:build` / `docker:up` | Build the image / `docker compose up` |
| `clean` | Remove `bin/`, `internal/webui/dist/` |

**Done when:** `task ci` passes on both Windows and Linux.

### 8. Docker

**Dockerfile (multi-stage):**
1. `node:lts-alpine`: `npm ci`, `npm run build` → `internal/webui/dist`.
2. `golang:<stable>-alpine`: `CGO_ENABLED=0 go build -tags embedui -trimpath -ldflags "-s -w -X main.version=$VERSION"`.
3. `gcr.io/distroless/static-debian12:nonroot`:
   - `ENV FA_DATA_DIR=/data FA_ADDR=0.0.0.0:8080`
   - `EXPOSE 8080`
   - `HEALTHCHECK CMD ["/financeanalysis", "healthcheck"]`

**compose.yaml:**
- `ports: ["127.0.0.1:8080:8080"]`
- `volumes: ["./data:/data"]`
- `user: "${UID:-1000}:${GID:-1000}"`: on Linux, files in the bind mount are then owned by the host user and stay easy to copy. Docker Desktop on Windows ignores this.
- `restart: unless-stopped`

`.dockerignore`: `node_modules`, `data`, `bin`, `.git`, build outputs.

**Done when:**
- `docker compose up` serves the app at `http://127.0.0.1:8080`.
- Create database writes `./data/finance.sqlite` on the host.
- The file opens in any SQLite tool.
- This holds on both Windows (Docker Desktop) and Linux.

### 9. Tests

**Go**
- `config`: defaults and env overrides.
- `db`:
  - Open sets the pragmas (`foreign_keys=1`, `journal_mode=delete`).
  - Migrating an empty DB reaches the latest version, with 4 categories seeded.
  - Migrating an up-to-date DB is a no-op and takes no backup.
  - An existing DB with pending migrations gets a backup first.
  - A schema newer than the binary returns `ErrSchemaTooNew`.
  - Up → down → up round trip (dev safety).
  - Integrity and FK checks pass.
  - Every DB in `testdata/fixtures/db/` migrates to latest (empty in M0; the mechanism is in place).
- `backup`: file created and readable; retention keeps the newest 20.
- `server` (via `httptest`):
  - status `missing`/`ready`;
  - create → `201`, then `409`;
  - unknown API route → JSON `404`;
  - SPA fallback serves `index.html` for `/statements`;
  - CSP header present;
  - oversized body rejected.

**Frontend**
- Vitest: `api.ts` error parsing.
- Playwright smoke test against `bin/financeanalysis{{exeExt}}` with a temporary `FA_DATA_DIR`:
  - load `/` → shows "no database";
  - click Create → shows the schema version;
  - reload `/statements` → the SPA still loads (fallback works).

All tests use temp directories; nothing touches `./data`.

### 10. CI (GitHub Actions)

- **Job `ci`**, matrix `ubuntu-latest` and `windows-latest`: set up Go and Node with caching, then `go tool task setup`, then `go tool task ci`.
- **Job `docker`** (Ubuntu): build the image, run it with a temp volume, poll `/api/status`, then `POST /api/database` and assert `ready`.
- No `-race` (it needs cgo on Windows). It can be added for the Ubuntu job only, later.

**Done when:** both jobs are green on a pull request.

### 11. Migration guide (`docs/migrations.md`)

Written per PRD section 9.2:
- One logical change per migration; naming convention `NNNNN_verb_object.sql`.
- Never edit an applied or released migration; fix forward.
- Down migrations exist for development; backups are the user-data rollback.
- SQLite `ALTER TABLE` limits, plus a copy-paste template of the 12-step table rebuild inside a goose migration (`-- +goose NO TRANSACTION` handling for `PRAGMA foreign_keys`, `foreign_key_check`).
- Schema changes vs. idempotent data backfills.
- Reference data (`category`) changes only via migrations.
- Use `STRICT` tables and explicit `CHECK` constraints for enums.
- Testing: empty → latest, fixture → latest, integrity/FK checks; how to add a fixture DB when cutting a release.
- Checklist: migration → `task generate` → Go code → tests/fixtures → update the PRD data model table.

**Done when:** the guide exists and `00001_init.sql` follows it.

## Definition of done (M0)

- [ ] `task setup && task dev` gives a working app with hot reload of the frontend.
- [ ] `task ci` is green locally on Windows and Linux.
- [ ] GitHub Actions is green for the matrix and the Docker job.
- [ ] `docker compose up` works on Windows and Linux; the DB file is on the host in `./data` and is a single file (no `-wal`/`-shm`).
- [ ] The browser makes no requests to anything but the app's own origin (verified in devtools; enforced by CSP).
- [ ] `docs/migrations.md` is written.

## Out of scope for M0

Welcome wizard, accounts, subcategories, import (all M1); Download/Replace database UI (M1); ECharts (M5); native release binaries (Later).

## Risks

| Risk | Mitigation |
|---|---|
| `sqlc` needs cgo on Windows | Verify in step 2; fall back to the release binary or Docker image. |
| SvelteKit CSP hashes not applied to the SPA fallback page | Verify in step 5; fall back to `'unsafe-inline'` for scripts only, keeping `connect-src 'self'`. |
| Bind-mount permissions on Linux | `user:` in compose; documented in the README. |
| The Paraglide setup changes between versions | Isolated in `web/`; swap for svelte-i18n if it causes friction. |
