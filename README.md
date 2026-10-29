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
