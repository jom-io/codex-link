# Setup

Repository: https://github.com/jom-io/codex-link

## Published release

Download `scripts/install.sh` from a reviewed tag or commit. Run it locally; it downloads the matching Mac release, verifies the published SHA-256, and installs the CLI to `~/.local/bin` and skill to `${CODEX_HOME:-~/.codex}/skills/codex-link`. Set `CODEX_LINK_VERSION=v0.1.0` to pin a release. Set `CODEX_LINK_INSTALL_REF=v0.1.0` when using this skill's installer wrapper. Checksums detect download corruption; they are not a separate publisher signature. macOS binaries are currently unsigned and not notarized.

## Source fallback

```sh
git clone https://github.com/jom-io/codex-link.git
cd codex-link
git checkout v0.1.0
sh scripts/install.sh --source "$PWD"
```

Requires Go 1.25+. A source ZIP requires extraction and the same build step. Do not assume an untagged branch is a stable release.

## Credentials

Copy `config.example.yaml` to `config.local.yaml` and let the user edit locally. Protect the filled file with `chmod 600`. Import using `codex-link init --config /absolute/path/config.local.yaml`. Two Macs share the workspace name, random pairing key, OSS bucket/prefix; their device IDs are generated independently and their display names should differ. Redis credentials may differ if ACLs permit access to the same namespace. Use TLS over public networks.

The first provider implementation is Aliyun OSS. Use an HTTPS regional endpoint, private bucket and prefix-restricted RAM credentials. STS additionally needs its security token and manual renewal before expiration. If Redis only supports plaintext, the user must explicitly set `tls: false` and `allow_insecure: true`; payload encryption does not protect the Redis password in transit.

`doctor --files` tests a small encrypted OSS upload/download and attempts cleanup. No cloud credentials ship with the package. Delete or secure the setup YAML after importing. `config.json` contains connection settings but no secrets. Keychain service: `io.jom.codex-link`, account: generated device ID.

Start with `codex-link daemon start`. Inspect `status`, `peers`, and the local `daemon.log`. Keychain must be accessible in the logged-in user's session. On reconfiguration, `daemon stop`, import YAML again, then `daemon start`.

Rollback: stop the daemon, restore `~/.local/bin/codex-link.previous` and the matching `.previous` skill directory, then start. `scripts/uninstall.sh` removes the application and LaunchAgent but preserves data and credentials.
