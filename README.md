# pgfmt

A PostgreSQL SQL formatter. Reads SQL from stdin, outputs formatted SQL to stdout.

Uses [libpg_query](https://github.com/pganalyze/pg_query_go) to parse SQL with the actual PostgreSQL parser, so it handles everything the real parser handles — including PL/pgSQL function bodies.

Try it in the browser: **[pgfmt playground](https://middle-management.github.io/pgfmt/)**.

## Robustness

pgfmt is designed to never lose or corrupt SQL:

- Statements and expressions the formatter doesn't explicitly support fall back to libpg_query's deparser, which emits valid (if plainly formatted) SQL rather than dropping anything.
- PL/pgSQL statements the formatter doesn't recognize keep their original body text verbatim.
- The test suite sweeps the **entire PostgreSQL 17 regression corpus** (~44,000 statements) and verifies that every formatted statement still parses, preserves its AST, and formats idempotently. The handful of known deviations (currently 48, all benign deparse artifacts of legacy syntaxes) are pinned in a baseline that CI enforces.

## Install

**Homebrew** (installs both `pgfmt` and `pgfmt-lsp`):

```bash
brew install middle-management/tap/pgfmt
```

**Go**:

```bash
go install github.com/middle-management/pgfmt@latest
```

## Usage

```bash
echo "SELECT id,name FROM users WHERE active=true" | pgfmt
```

```bash
pgfmt < query.sql
```

## LSP Server

pgfmt includes an LSP server that provides formatting and diagnostics for SQL files.

### Install

Already included with the Homebrew formula above, or install separately with Go:

```bash
go install github.com/middle-management/pgfmt/cmd/pgfmt-lsp@latest
```

### Editor Setup

**Neovim** (via `nvim-lspconfig`):

```lua
vim.lsp.config['pgfmt'] = {
  cmd = { 'pgfmt-lsp' },
  filetypes = { 'sql' },
  root_markers = { '.git' },
}
vim.lsp.enable('pgfmt')
```

**VS Code** (via a generic LSP client like `vscode-languageclient`):

```json
{
  "pgfmt.server.path": "pgfmt-lsp"
}
```

**Zed** — install the [pgfmt Zed extension](https://github.com/middle-management/pgfmt/tree/main/zed-pgfmt) from the extension registry. It bundles `pgfmt-lsp` and downloads it automatically — no manual setup required.

Then enable formatting in `.zed/settings.json` or `~/.config/zed/settings.json`:

```json
{
  "languages": {
    "SQL": {
      "formatter": "language_server",
      "format_on_save": "on"
    }
  }
}
```

See the [extension README](zed-pgfmt/README.md) for advanced configuration, including how to use a custom binary path.

### Features

- **Formatting** — format SQL files using `textDocument/formatting`
- **Diagnostics** — parse errors reported on open and change

## Build from source

```bash
git clone https://github.com/middle-management/pgfmt.git
cd pgfmt
go build -o pgfmt .
go build -o pgfmt-lsp ./cmd/pgfmt-lsp
```

Prebuilt binaries for Linux and macOS (amd64 and arm64) are attached to each [release](https://github.com/middle-management/pgfmt/releases).
