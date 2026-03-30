use zed_extension_api::{self as zed, LanguageServerId, Result};

struct PgfmtExtension;

impl zed::Extension for PgfmtExtension {
    fn new() -> Self {
        PgfmtExtension
    }

    fn language_server_command(
        &mut self,
        _language_server_id: &LanguageServerId,
        worktree: &zed::Worktree,
    ) -> Result<zed::Command> {
        // Look for pgfmt-lsp in PATH
        let path = worktree
            .which("pgfmt-lsp")
            .ok_or_else(|| "pgfmt-lsp not found in PATH. Install with: go install github.com/middle-management/pgfmt/cmd/pgfmt-lsp@latest".to_string())?;

        Ok(zed::Command {
            command: path,
            args: vec![],
            env: Default::default(),
        })
    }
}

zed::register_extension!(PgfmtExtension);
