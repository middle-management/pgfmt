# pgfmt for Zed

A [Zed](https://zed.dev) extension that provides PostgreSQL SQL formatting and diagnostics via [pgfmt-lsp](https://github.com/middle-management/pgfmt).

## Features

- **Formatting** — format SQL files on demand or automatically on save
- **Diagnostics** — SQL parse errors shown inline as you type

## Installation

Install the extension from the Zed extension registry:

1. Open the command palette (`cmd+shift+p`)
2. Run **Extensions: Install Extension**
3. Search for **pgfmt** and install it

The `pgfmt-lsp` binary is downloaded automatically from [GitHub Releases](https://github.com/middle-management/pgfmt/releases) for your platform — no manual installation required.

## Configuration

### Format on save

Add this to your Zed settings (`cmd+,`) or your project's `.zed/settings.json`:

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

### Custom binary path

If you'd prefer to manage `pgfmt-lsp` yourself (e.g. via `go install` or a version manager like mise), you can point the extension at your own binary and bypass the auto-download entirely:

```json
{
  "lsp": {
    "pgfmt-lsp": {
      "binary": {
        "path": "/Users/you/go/bin/pgfmt-lsp"
      }
    }
  }
}
```

Install the binary manually with:

```bash
go install github.com/middle-management/pgfmt/cmd/pgfmt-lsp@latest
```

> **Note:** Zed does not inherit your shell's `$PATH`, so tools managed by mise, asdf, or similar may not be found automatically. Use an absolute path in the `binary.path` setting if the LSP fails to start.

### Binary resolution order

When no explicit `binary.path` is configured, the extension resolves the binary as follows:

1. `pgfmt-lsp` on the system `PATH` (useful during local development)
2. A previously-downloaded binary cached in the extension directory
3. Downloaded from the latest [GitHub Release](https://github.com/middle-management/pgfmt/releases) for your platform

## Supported platforms

| OS    | Architecture |
|-------|--------------|
| macOS | arm64 (Apple Silicon) |
| macOS | amd64 (Intel) |
| Linux | amd64 |
| Linux | arm64 |

## Contributing

See the [pgfmt repository](https://github.com/middle-management/pgfmt) for source code, issue tracking, and contribution guidelines.