# Setup

Repository: https://github.com/jom-io/codex-link

## Published release

Download `scripts/install.sh` from a reviewed tag or commit. Run it locally; it downloads the matching Mac release, verifies the published SHA-256, and installs the CLI to `~/.local/bin` and skill to `${CODEX_HOME:-~/.codex}/skills/agent-link`. Set `AGENT_LINK_VERSION=v0.2.0` to pin this stable release. Set `AGENT_LINK_INSTALL_REF=v0.2.0` when using this skill's installer wrapper. Checksums detect download corruption; they are not a separate publisher signature. macOS binaries are currently unsigned and not notarized.

## Source fallback

```sh
git clone https://github.com/jom-io/codex-link.git agent-link
cd agent-link
git fetch origin tag v0.2.0
git switch --detach v0.2.0
AGENT_LINK_VERSION=v0.2.0 sh scripts/install.sh --source "$PWD"
```

Requires Go 1.25+. A source ZIP requires extraction and the same build step. Do not assume an untagged branch is a stable release.

## Credentials

Copy `config.example.yaml` to `config.local.yaml` and let the user edit locally. Protect the filled file with `chmod 600`. Import using `agent-link init --config /absolute/path/config.local.yaml`. Two Macs share the workspace name and OSS bucket/prefix; their device IDs are generated independently and their display names should differ. No shared encryption key is configured. Device keys are generated into Keychain; first contact opens matching-code confirmation dialogs on both Macs. Redis credentials may differ if ACLs permit access to the same namespace. Use TLS over public networks.

The first provider implementation is Aliyun OSS. Use an HTTPS regional endpoint, private bucket and prefix-restricted RAM credentials. STS additionally needs its security token and manual renewal before expiration. If Redis only supports plaintext, the user must explicitly set `tls: false` and `allow_insecure: true`; payload encryption does not protect the Redis password in transit.

`doctor --files` tests a small encrypted OSS upload/download and attempts cleanup. No cloud credentials ship with the package. Delete or secure the setup YAML after importing. `config.json` contains connection settings but no secrets. Keychain service: `io.jom.codex-link`, account: generated device ID.

Start with `agent-link daemon start`. Inspect `status`, `peers`, and the local `daemon.log`. Keychain must be accessible in the logged-in user's session. On reconfiguration, `daemon stop`, import YAML again, then `daemon start`.

Rollback: stop the daemon, restore both binaries and matching skills from the installer-reported `$BIN_DIR/.agent-link-backups/<old-binary-sha256>/` snapshot, then start. `scripts/uninstall.sh` removes the commands and LaunchAgent but preserves skills, backups, history, cached files and credentials.

For a complete second-Mac Chinese walkthrough, read https://github.com/jom-io/codex-link/blob/master/docs/installation.zh-CN.md . The guide may contain documentation updates newer than the pinned binary release.

## Compatible upgrade

Keep the existing codex-link data directory, Keychain service `io.jom.codex-link`, LaunchAgent label, Redis protocol v2 and OSS prefix. `AGENT_LINK_HOME` takes precedence over legacy `CODEX_LINK_HOME`. Both commands connect to the same socket. Do not rename credential entries or copy private keys. Install a verified extracted package with `sh scripts/install.sh --package /absolute/extracted/package`; stop/start only the managed link daemon. Installer snapshots are kept under `$BIN_DIR/.agent-link-backups/<old-binary-sha256>/`. The CLI supports `AGENT_LINK_*` credential environment names and legacy `CODEX_LINK_*`.
