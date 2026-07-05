# CLAUDE.md

## Project Overview

pgfmt is a PostgreSQL SQL formatter written in Go. It reads SQL from stdin, parses it using `pg_query_go` (libpg_query), and outputs formatted SQL to stdout.

## Build & Test Commands

```bash
go build ./...          # Build everything
go test ./...           # Run all tests
go test -v ./...        # Run all tests (verbose)
go test -run TestName   # Run a specific test
```

There is no Makefile; use `go` commands directly.

## Project Structure

- `main.go` — CLI entry point, reads stdin and prints formatted SQL
- `printer/` — Core formatting logic (`printer.go`, `printer_test.go`)
- `testdata/fixtures/` — Input SQL fixture files for integration tests
- `testdata/fixtures/golden/` — Expected output (golden files) for fixture tests

## Testing

- **Unit tests** in `printer/printer_test.go` cover individual SQL constructs
- **Integration tests** in `main_test.go` compare formatted output against golden files in `testdata/fixtures/golden/`
- CI verifies golden files are up to date via `git diff --exit-code testdata/fixtures/golden/`
- When adding new SQL formatting support, prefer golden fixture coverage over custom unit tests
- Only use unit tests when a fixture can't adequately cover the case
- **Corpus test** in `corpus_test.go` sweeps the PostgreSQL regression suite
  (pinned to the release matching `pg_query_go`) and classifies every statement
  (panic / format-error / output-invalid / roundtrip-diff / not-idempotent)
  against `testdata/corpus_baseline.txt`. Run with
  `PGFMT_CORPUS=1 go test -run TestPostgresRegressionCorpus .`; after fixing
  formatter gaps, refresh the baseline with `PGFMT_UPDATE_BASELINE=1`
  (shrinking numbers are the goal — never grow an entry to make a change pass)

## Deparse Fallback

Any SQL statement type not explicitly handled by the printer falls back to
`pg_query.Deparse`, which emits valid (but unformatted/canonical) SQL. This
ensures pgfmt never produces empty output for valid SQL.

## Playground

The browser playground uses two WASM modules:
- **pg-query-emscripten** — Emscripten build of libpg_query for SQL parsing/deparsing
- **pgfmt-print.wasm** — Go WASI build of the printer

### Building pg-query-emscripten

The Emscripten build lives in `third_party/pg-query-emscripten` (git submodule). It must match the libpg_query version used by `pg_query_go` in `go.mod`. To check versions:

```bash
# Go side — look for LIB_PG_QUERY_TAG in the module
grep LIB_PG_QUERY_TAG $(go env GOMODCACHE)/github.com/pganalyze/pg_query_go/v6@*/Makefile

# Emscripten side
head -1 third_party/pg-query-emscripten/Makefile
```

To rebuild after changes to `entry.cpp` or `module.js`:

```bash
brew install emscripten  # if not installed
cd third_party/pg-query-emscripten
make clean && make
cp pg_query.js ../../playground/pg_query.js
```

### Updating libpg_query version

When upgrading `pg_query_go` in `go.mod`, update the Emscripten build to match:

1. Check the new `LIB_PG_QUERY_TAG` from the Go module (see above)
2. Edit `third_party/pg-query-emscripten/Makefile` — update `LIB_PG_QUERY_TAG`
3. Rebuild: `make clean && make`
4. Copy: `cp pg_query.js ../../playground/pg_query.js`
5. Run playground tests: `cd playground && npx playwright test`

### Building the Go WASM printer

```bash
GOOS=wasip1 GOARCH=wasm go build -o playground/pgfmt-print.wasm ./cmd/pgfmt-print
```

### Playground tests

```bash
cd playground
npm ci && npx playwright install --with-deps chromium  # first time
npx playwright test
```

## Code Conventions

- Go 1.23+
- Standard Go formatting (`gofmt`)
- No external linter config; rely on `go vet` and the compiler
- The printer uses a `PrintNode` method that switches on protobuf node types from `pg_query_go/v6`
