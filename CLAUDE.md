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
- When adding new SQL formatting support, add both unit tests and fixture coverage

## Missing / Unsupported Constructs

The following SQL constructs are parsed by `pg_query_go` but not yet handled by the printer:

### Expression nodes
- **XmlExpr** — XML functions (`XMLPARSE`, `XMLROOT`, `XMLELEMENT`, etc.)

### DDL not yet handled
- **ALTER SEQUENCE**
- **CREATE TRIGGER**
- **CREATE DOMAIN**
- **REINDEX / CLUSTER**

## Code Conventions

- Go 1.23+
- Standard Go formatting (`gofmt`)
- No external linter config; rely on `go vet` and the compiler
- The printer uses a `PrintNode` method that switches on protobuf node types from `pg_query_go/v6`
