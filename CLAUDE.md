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
- `printer/` — Core formatting logic, split by area:
  - `printer.go` — main `writeNode` switch on protobuf node types
  - `printer_ddl.go`, `printer_dml.go`, `printer_expr.go`, `printer_json.go` — DDL / DML / expression / JSON constructs
  - `printer_util.go` — shared helpers (identifier quoting, dollar quoting, type names, ...)
  - `plpgsql.go`, `plpgsql_ast.go` — PL/pgSQL function-body formatting (own JSON AST from `ParsePlPgSqlToJSON`)
  - `keywords.go` — reserved-keyword list driving `quoteIdentifier`
  - `augment.go`, `augmented.go` — precomputed deparse cache for the WASI build (no native deparse there)
  - `parser_cgo.go` / `parser_wasi.go` — build-tagged parse/deparse backends
- `cmd/pgfmt-lsp/` — LSP server binary; `lsp/` — its implementation
- `cmd/pgfmt-print/` — WASI printer binary for the playground
- `playground/` — browser playground (GitHub Pages), Playwright tests
- `zed-pgfmt/` — Zed editor extension (Rust)
- `testdata/fixtures/` — Input SQL fixture files for integration tests
- `testdata/fixtures/golden/` — Expected output (golden files) for fixture tests
- `testdata/corpus_baseline.txt` — pinned deviations for the corpus test

## Testing

- **Unit tests** in `printer/printer_test.go` cover individual SQL constructs
- **Integration tests** in `main_test.go` compare formatted output against golden files in `testdata/fixtures/golden/`
- CI verifies golden files are up to date via `git diff --exit-code testdata/fixtures/golden/`
- When adding new SQL formatting support, prefer golden fixture coverage over custom unit tests
- Only use unit tests when a fixture can't adequately cover the case
- **Idempotency/AST tests** in `idempotency_test.go` assert over all fixtures
  that `Format(Format(x)) == Format(x)` and that formatting preserves the AST
  (parse → deparse comparison, normalizing function bodies and option order)
- **Corpus test** in `corpus_test.go` sweeps the PostgreSQL regression suite
  (pinned to the release matching `pg_query_go`) and classifies every statement
  (panic / format-error / output-invalid / roundtrip-diff / not-idempotent)
  against `testdata/corpus_baseline.txt`. Run with
  `PGFMT_CORPUS=1 go test -run TestPostgresRegressionCorpus .`; after fixing
  formatter gaps, refresh the baseline with `PGFMT_UPDATE_BASELINE=1`
  (shrinking numbers are the goal — never grow an entry to make a change pass).
  The suite downloads once into gitignored `testdata/corpus/`; CI caches it
  and runs the test as a separate `corpus` job
- **WASI test** in `wasi_test.go` checks the WASI build of the printer
  produces output equivalent to the native build for the fixtures (the WASI
  build has no native deparse, so deparse-dependent constructs are the usual
  source of divergence — see Deparse Fallback below)

## Deparse Fallback

The printer must never drop or corrupt SQL. Unhandled constructs fall back to
`pg_query.Deparse` at the narrowest scope that works:

- **Expression** — `deparseExprFallback` renders a single expression via a
  synthetic one-target SELECT
- **FROM item** — `deparseRangeFallback` renders a range node (JSON_TABLE,
  XMLTABLE, ...) via a synthetic `SELECT * FROM` wrapper
- **Whole statement** — `tryStatementFallback` deparses the entire statement;
  used when a sub-clause can't be rendered safely in isolation (e.g. an
  unsupported ALTER TABLE subcommand)
- **PL/pgSQL** — unknown statement types make the body formatter return an
  error, and the whole function body is kept verbatim
  (`writeRawPLpgSQLBody`), newline-normalized so output stays idempotent

The result is valid (if unformatted/canonical) SQL, never empty output.

The corpus test enforces this: any change that makes output invalid, changes
the AST, or breaks idempotency on the PostgreSQL regression suite fails CI
against `testdata/corpus_baseline.txt`.

The WASI build (`parser_wasi.go`) has **no parser or deparser at all** — it
consumes an "augmented AST" JSON (parse tree plus per-statement precomputed
`deparsed` text and PL/pgSQL body cache) via `FormatAugmented`. The augmented
AST is built by `printer/augment.go` on native builds and by
`playground/worker.js` in the browser (using pg-query-emscripten). The
precomputed text covers whole-statement fallback, but expression- and
range-level deparse fallbacks cannot run on WASI, so those constructs render
differently in the playground; keep such SQL out of fixtures checked by
`wasi_test.go`.

## Playground

The browser playground uses two WASM modules:
- **pg-query-emscripten** — Emscripten build of libpg_query for SQL parsing/deparsing
- **pgfmt-print.wasm** — Go WASI build of the printer

JS dependencies (the browser WASI shim and speed-highlight) are vendored
under `playground/vendor/` — the playground must not fetch anything from
third-party CDNs at runtime.

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

## Releases

Releases are fully automated via GitHub Actions:

1. Dispatch **`prepare-release.yml`** with the version (e.g. `0.4.0`). It
   bumps version strings (`lsp/server.go`, `zed-pgfmt/extension.toml`,
   `zed-pgfmt/Cargo.toml`, `zed-pgfmt/src/lib.rs`) and opens a
   `release/vX.Y.Z` PR (authored via the `RELEASE_TOKEN` PAT so CI runs)
2. Merge the release PR when CI is green
3. **`create-release.yml`** fires on the merged PR: creates the tag and
   GitHub release with generated notes, builds `pgfmt` + `pgfmt-lsp` binaries
   for linux/darwin × amd64/arm64, and calls `zed-extension-bump.yml`

**`zed-extension-bump.yml`** opens (or refreshes) a version-bump PR against
`zed-industries/extensions` from the fork `middle-management/extensions`.
Important details:

- The commit is authored as the owner of the `ZED_COMMITTER_TOKEN` PAT, not
  `github-actions[bot]` — Zed's CLA bot checks commit **authors**, and bots
  cannot sign the CLA. The PAT owner must have signed https://zed.dev/cla
- The branch (`pgfmt-lsp-vX.Y.Z`) is force-pushed, so re-running refreshes an
  open registry PR in place; the run is idempotent and safe to retry via
  manual `workflow_dispatch` with the tag as input
- If the CLA bot still blocks the registry PR, comment `@cla-bot check` on it
  after the CLA is signed

The Homebrew tap (`middle-management/homebrew-tap`, `Formula/pgfmt.rb`) pins
the release version and per-binary sha256 sums. It updates itself nightly
from the latest release — no action needed here; expect up to a day's lag
between a release and `brew install` picking it up.

## Code Conventions

- Go 1.23+
- Standard Go formatting (`gofmt`)
- No external linter config; rely on `go vet` and the compiler
- The printer uses a `PrintNode` method that switches on protobuf node types from `pg_query_go/v6`
