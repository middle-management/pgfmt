# pgfmt for Zed

A Zed extension that provides PostgreSQL SQL formatting and diagnostics via [pgfmt](https://github.com/middle-management/pgfmt).

## Features

- **Formatting** — format SQL files on demand or on save
- **Diagnostics** — SQL parse errors shown inline

## Prerequisites

Install the pgfmt LSP server:

```bash
go install github.com/middle-management/pgfmt/cmd/pgfmt-lsp@latest
```

Make sure `pgfmt-lsp` is in your `PATH`.

## Configuration

To use pgfmt as your default SQL formatter, add to your Zed settings:

```json
{
  "languages": {
    "SQL": {
      "formatter": "language_server",
      "language_servers": ["pgfmt-lsp"]
    }
  }
}
```
