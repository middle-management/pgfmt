use zed_extension_api::{self as zed, LanguageServerId, Result, settings::LspSettings};

const BINARY_NAME: &str = "pgfmt-lsp";
const GITHUB_REPO: &str = "middle-management/pgfmt";
// Keep this in sync with extension.toml. The release workflow updates both automatically.
const VERSION: &str = "0.2.0";

struct PgfmtExtension {
    cached_binary_path: Option<String>,
}

impl PgfmtExtension {
    fn language_server_binary(
        &mut self,
        language_server_id: &LanguageServerId,
        worktree: &zed::Worktree,
    ) -> Result<zed::Command> {
        // 1. Honour explicit user settings:
        //
        //    {
        //      "lsp": {
        //        "pgfmt-lsp": {
        //          "binary": {
        //            "path": "/path/to/pgfmt-lsp",
        //            "arguments": []
        //          }
        //        }
        //      }
        //    }
        if let Ok(lsp_settings) = LspSettings::for_worktree(BINARY_NAME, worktree) {
            if let Some(binary) = lsp_settings.binary {
                if let Some(path) = binary.path {
                    return Ok(zed::Command {
                        command: path,
                        args: binary.arguments.unwrap_or_default(),
                        env: Default::default(),
                    });
                }
            }
        }

        // 2. If pgfmt-lsp is already on PATH (e.g. via `go install` during development),
        //    use it immediately — no download needed.
        if let Some(path) = worktree.which(BINARY_NAME) {
            return Ok(zed::Command {
                command: path,
                args: vec![],
                env: Default::default(),
            });
        }

        // 3. Return the cached path if the binary still exists on disk.
        if let Some(path) = &self.cached_binary_path {
            if std::path::Path::new(path).exists() {
                return Ok(zed::Command {
                    command: path.clone(),
                    args: vec![],
                    env: Default::default(),
                });
            }
        }

        // 4. Download the binary from the GitHub release matching this extension's version.
        zed::set_language_server_installation_status(
            language_server_id,
            &zed::LanguageServerInstallationStatus::CheckingForUpdate,
        );

        let release = zed::github_release_by_tag_name(GITHUB_REPO, &format!("v{VERSION}"))?;

        let (os, arch) = zed::current_platform();

        let os_str = match os {
            zed::Os::Mac => "darwin",
            zed::Os::Linux => "linux",
            zed::Os::Windows => return Err("pgfmt-lsp does not support Windows".into()),
        };

        let arch_str = match arch {
            zed::Architecture::Aarch64 => "arm64",
            zed::Architecture::X8664 => "amd64",
            zed::Architecture::X86 => return Err("pgfmt-lsp does not support x86".into()),
        };

        let asset_name = format!("{BINARY_NAME}-{os_str}-{arch_str}");

        let asset = release
            .assets
            .iter()
            .find(|a| a.name == asset_name)
            .ok_or_else(|| {
                format!(
                    "no release asset found for your platform ({asset_name}). \
                     Install manually with: go install github.com/middle-management/pgfmt/cmd/pgfmt-lsp@latest"
                )
            })?;

        // Store the binary under a versioned path so stale downloads are easy to detect.
        // Strip the leading "v" from the tag name (e.g. "v0.1.2" -> "0.1.2").
        let version_dir = release
            .version
            .strip_prefix('v')
            .unwrap_or(&release.version);
        let binary_dir = format!("{BINARY_NAME}/{version_dir}");
        let binary_path = format!("{binary_dir}/{BINARY_NAME}");

        if !std::path::Path::new(&binary_path).exists() {
            std::fs::create_dir_all(&binary_dir)
                .map_err(|e| format!("failed to create directory {binary_dir}: {e}"))?;
            zed::set_language_server_installation_status(
                language_server_id,
                &zed::LanguageServerInstallationStatus::Downloading,
            );

            zed::download_file(
                &asset.download_url,
                &binary_path,
                zed::DownloadedFileType::Uncompressed,
            )
            .map_err(|e| format!("failed to download {asset_name}: {e}"))?;

            zed::make_file_executable(&binary_path)
                .map_err(|e| format!("failed to make {BINARY_NAME} executable: {e}"))?;

            // Remove any previously-downloaded versions to keep the extension dir tidy.
            if let Ok(entries) = std::fs::read_dir(BINARY_NAME) {
                for entry in entries.flatten() {
                    if entry.file_name().to_string_lossy() != version_dir {
                        std::fs::remove_dir_all(entry.path()).ok();
                    }
                }
            }
        }

        self.cached_binary_path = Some(binary_path.clone());

        Ok(zed::Command {
            command: binary_path,
            args: vec![],
            env: Default::default(),
        })
    }
}

impl zed::Extension for PgfmtExtension {
    fn new() -> Self {
        PgfmtExtension {
            cached_binary_path: None,
        }
    }

    fn language_server_command(
        &mut self,
        language_server_id: &LanguageServerId,
        worktree: &zed::Worktree,
    ) -> Result<zed::Command> {
        self.language_server_binary(language_server_id, worktree)
    }
}

zed::register_extension!(PgfmtExtension);
