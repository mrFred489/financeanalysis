# FinanceAnalysis

A locally hosted web application for cataloging and visualizing personal spending.

## Prerequisites

- Go (current stable, >= 1.24)
- Node.js LTS + npm
- Docker (optional, for containerized deployment)
- Task (`go-task`) - or use `go tool task`

## Quick Start

### Native Development

```bash
# Install dependencies
task setup

# Run the development server
task dev
```

The application will be available at `http://127.0.0.1:8080`.

The frontend dev server runs on port 5173 with hot reload.

### Dev slots (run several environments at once)

By default `task dev` uses the `main` slot: data in `./data`, API on
`127.0.0.1:8080`, web on `127.0.0.1:5173`. To run an isolated environment next
to it (e.g. for an agent), pick a slot name:

```bash
SLOT=myagent go tool task dev
SLOT=myagent go tool task dev:ports   # print that slot's data dir and URLs
```

Named slots store their data in `.dev/<slot>/` (gitignored) and get
deterministic ports derived from the slot name (API in 8081-8179, web in
5174-5272), so several slots can run concurrently without locking each other's
database. Override the ports explicitly with `API_PORT` / `WEB_PORT`, and wipe
a slot with `go tool task dev:reset SLOT=myagent`.

To run against a completely different data directory instead of a slot, set
`DATA_DIR` (or equivalently `SLOT_DATA_DIR`) and optionally pick your own ports
with `API_PORT` / `WEB_PORT`:

```bash
# custom data dir, ports derived from the directory path
DATA_DIR=~/financeanalysisdata go tool task dev

# custom data dir AND custom ports — the full example
DATA_DIR=~/financeanalysisdata API_PORT=8090 WEB_PORT=5180 go tool task dev

# same thing via a slot name; SLOT_DATA_DIR overrides .dev/<slot>
SLOT=custom SLOT_DATA_DIR=~/financeanalysisdata API_PORT=8090 WEB_PORT=5180 go tool task dev
```

You can combine this with a slot; the slot only picks the default data dir
(`.dev/<slot>/`) and the port hash when no custom dir/ports are given.
Check what a run will use before starting it:

```bash
SLOT=custom DATA_DIR=~/financeanalysisdata API_PORT=8090 WEB_PORT=5180 go tool task dev:ports
# slot=custom data_dir=/home/you/financeanalysisdata api=http://127.0.0.1:8090 web=http://127.0.0.1:5180
```

When `DATA_DIR`/`SLOT_DATA_DIR` is set, the derived ports are hashed from the
directory path unless `API_PORT` / `WEB_PORT` are given. `dev:reset` never
deletes a custom dir outside `.dev/` unless you set `CONFIRM=1`.

### Docker

```bash
# Build and run
docker compose up
```

The application will be available at `http://127.0.0.1:8080`.

Data is persisted in `./data/finance.sqlite` on the host.

## Project Layout

- `cmd/financeanalysis/`: Go entrypoint
- `internal/`: Go application code
- `web/`: SvelteKit frontend
- `docs/`: Documentation
- `Taskfile.yml`: Cross-platform task runner

## Configuration

Environment variables:
- `FA_DATA_DIR`: Database directory (default: `./data`)
- `FA_ADDR`: Server address (default: `127.0.0.1:8080`)
- `FA_LOG_LEVEL`: Log level (default: `info`)

## License

MIT
