# pgfmt

A PostgreSQL SQL formatter. Reads SQL from stdin, outputs formatted SQL to stdout.

Uses [libpg_query](https://github.com/pganalyze/pg_query_go) to parse SQL with the actual PostgreSQL parser.

## Install

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

**Zed** (in `~/.config/zed/settings.json`):

```json
{
  "lsp": {
    "pgfmt": {
      "binary": {
        "path": "pgfmt-lsp"
      }
    }
  },
  "languages": {
    "SQL": {
      "language_servers": ["pgfmt"]
    }
  }
}
```

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
