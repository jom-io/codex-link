# codex-link

[简体中文](README.zh-CN.md) · [MIT License](LICENSE)

An encrypted message and file bridge for Codex sessions on two Macs. One lightweight Go daemon per macOS user receives messages continuously; every local Codex window uses the same CLI, inbox, task history and downloaded files.

**Status:** first release. The application provides communication, not automatic remote command execution or automatic awakening of idle Codex chats. Aliyun OSS is the first supported file provider.

## What it does

- Automatic device identities, customizable names, first-contact pairing and session/conversation routing.
- Redis Streams with a durable SQLite outbox, retries, duplicate suppression and delivery receipts.
- Offline catch-up and local history shared by all windows under the same macOS user.
- Task claiming with renewable leases and transactional completion replies.
- Encrypted Aliyun OSS transfers, bounded-memory file processing and SHA-256 verification.
- Keychain credentials/device keys, mutual native pairing dialogs, a private Unix socket, LaunchAgent management and a Codex skill.
- GitHub Release installation, source installation, updates, rollback and uninstall.

## Architecture

```text
Mac A                                      Mac B
Codex windows                              Codex windows
     |                                          |
Codex skill -> CLI                         Codex skill -> CLI
     |                                          |
Unix socket -> Go daemon                   Unix socket -> Go daemon
     | SQLite + Keychain                        | SQLite + Keychain
     +------------- Redis Streams --------------+
     +------------- private Aliyun OSS ---------+

GitHub repository / Releases: source, skill and Mac binaries
```

No inbound public port is required on either Mac. Redis carries signed discovery/pairing events and encrypted application messages; OSS stores encrypted file frames. Routing IDs, device names/public keys, timing, sizes and object paths remain visible. Each device generates Ed25519/X25519 keys automatically. First contact requires confirmation on both devices; pairwise encryption keys are derived automatically and never copied by users. Session aliases are not an access-control boundary.

## Install

### Stable Release (recommended)

Download and inspect the installer from the chosen repository tag, then run it:

```sh
curl -fL https://raw.githubusercontent.com/jom-io/codex-link/v0.1.0/scripts/install.sh -o /tmp/codex-link-install.sh
CODEX_LINK_VERSION=v0.1.0 sh /tmp/codex-link-install.sh
export PATH="$HOME/.local/bin:$PATH"
codex-link version
```

The installer selects Apple Silicon (`arm64`) or Intel (`amd64`), checks the Release SHA-256 and installs the binary and skill. Published checksums detect corruption, not a separate publisher signature. Binaries are currently unsigned and not notarized. Download links work after the corresponding Release is published.

### Source or source ZIP

Requires Go 1.25+; normal Release installation needs no Go runtime or Python environment.

```sh
git clone https://github.com/jom-io/codex-link.git
cd codex-link
git checkout v0.1.0
sh scripts/install.sh --source "$PWD"
```

For a source ZIP, extract it and run the same source-install command. To develop before a release exists, build the checked-out `master` tree directly.

## Configure both Macs

Copy the template and edit it locally:

```sh
cp config.example.yaml config.local.yaml
chmod 600 config.local.yaml
codex-link init --config "$PWD/config.local.yaml"
codex-link doctor --files
codex-link daemon start
codex-link status
codex-link peers
```

`config.local.yaml` is gitignored. Never commit or paste a filled configuration into a chat. Both Macs use the same `workspace`, OSS bucket and prefix, and different device names. Each Mac automatically generates its identity and saves private keys in Keychain. There is no `workspace_key` configuration.

Template fields:

| Field | Meaning |
| --- | --- |
| `name`, `workspace` | ASCII letters/digits plus `.`, `_`, `-`; 1–64 characters |
| `redis.address` | `host:port`, without a URL scheme |
| `redis.username`, `redis.db` | ACL username and database number |
| `redis.tls` | TLS enabled for public Redis |
| `redis.allow_insecure` | Explicit opt-in when a deployment has no TLS; Redis credentials then travel in plaintext |
| `oss.endpoint`, `oss.region` | HTTPS Aliyun regional endpoint; region is inferred for standard endpoints |
| `oss.bucket`, `oss.prefix` | Private bucket and shared object prefix |
| `credentials.redis_password` | Redis password |
| `credentials.access_key_id`, `access_key_secret` | Prefix-restricted RAM credentials |
| `credentials.security_token` | Optional STS token; manual renewal is required |
| `max_file_bytes` | Default 1 GiB per file |
| `stream_max_len` | Approximate stream cap per device; default 10,000 entries, including receipts |
| `stream_ttl_hours` | Stream/dedup expiration after the last write; default 720 hours |

Set OSS endpoint and bucket to empty strings to use messages only. See [infrastructure configuration](docs/infrastructure.md) for Redis ACLs and OSS policies. `doctor --files` makes a small upload/download/delete probe, not a production durability test.

Initialization imports credentials into macOS Keychain (`io.jom.codex-link`, account = device ID). The persisted `config.json` contains no passwords or keys. Secure or delete the filled YAML after importing. Interactive `init` uses hidden secret prompts; `--secrets-env` is also available with a configuration file using `CODEX_LINK_REDIS_PASSWORD`, `CODEX_LINK_OSS_ACCESS_KEY_ID`, `CODEX_LINK_OSS_ACCESS_KEY_SECRET`, and optional `CODEX_LINK_OSS_SECURITY_TOKEN`.

## First-contact pairing: confirm on both Macs

Send normally, e.g. `codex-link send --to office-mac --text "Hello"`. If the devices are not paired, the message remains queued and each Mac shows a native confirmation dialog. Compare the six-digit code shown on BOTH Macs, check the intended device name/ID, and click Confirm on both. The application pins the peer identity, derives the encryption keys, and automatically sends the queued message. Rejecting or confirming only one side does not release it.

Adding a third Mac requires new pairwise confirmations; it does not inherit access to existing pairs. Requests expire after ten minutes; dialogs time out after two minutes. If a request expires or is rejected, deliberately initiate a new request. A reinstall that loses identity keys creates a new device ID and requires pairing again.

For headless/accessibility workflows, set `pairing_headless: true` and use the explicit local commands after human confirmation:

```sh
codex-link pair request --to office-mac
codex-link pair list
codex-link pair accept REQUEST_ID --code 123456
# or: codex-link pair reject REQUEST_ID --code 123456
```

Normal macOS use needs no CLI pairing step. First-contact file sends initiate pairing and return `pairing_pending`; retry the file command after confirmation. The skill handles that retry. Previously queued text/tasks send automatically.

## Send messages and cooperate

```sh
codex-link send --to office-mac --text "Please check your development environment"
codex-link send --to office-mac --kind task --session requester --conversation setup --text "Check Go version; report the result. Do not install anything."
codex-link inbox --after 0 --session worker --conversation setup
codex-link task claim TASK_ID --session worker --lease 900
codex-link task complete TASK_ID --session worker --text "Go version verified: ..."
codex-link wait --after 12 --session requester --conversation setup --timeout 30
codex-link history --after 0
codex-link get MESSAGE_ID
```

All communication commands emit JSON. IDs must precede flags. `--after` is a local SQLite sequence cursor; advance it after processing records. Reading never removes shared messages. Inbox/wait include incoming records; history includes both directions. Receipts are hidden. A wait lasts at most 60 seconds; default `--after 0` can return existing records, and `--after -1` deliberately starts at the current tail.

Use `--to-session` to route a message to a chosen alias; local inbox filtering by `--session` includes that alias and device-wide messages. Session aliases are provided by the calling Codex window, not inferred from Codex internals. Session filtering is not an access-control boundary.

`pending` means saved locally, `sent` means accepted by Redis, `delivered` means saved by the receiving daemon. Task execution is a separate step. A received task is data, not permission: Codex executes only within human-authorized scope. Renewable leases reduce simultaneous execution; a crashed worker may be replaced after lease expiry, so task actions should be idempotent.

## Transfer files

```sh
codex-link file send --to office-mac --path /absolute/path/report.zip --conversation setup
codex-link file fetch FILE_MESSAGE_ID
```

The receive command returns a verified local absolute path accessible to all windows under the same user. Directories must be archived explicitly; the application never automatically extracts or executes attachments. Uploads encrypt to a temporary local file before streaming to OSS. Downloads decrypt to a temporary file, check size and SHA-256, then rename into the cache. This bounds RAM usage but needs temporary disk space approximately equal to the encrypted upload or downloaded file size. Failed upload/message pairs can leave orphaned objects; configure OSS lifecycle cleanup.

## Use the Codex skill

The installer places [the skill](skills/codex-link/SKILL.md) in `${CODEX_HOME:-~/.codex}/skills/codex-link`. It becomes available in a subsequent Codex turn. Example:

> Use codex-link to ask office-mac to check its Go installation, and send its verified result back.

The native pairing dialog can appear even when no chat is active. The daemon receives even when no chat is active. An active Codex session can wait for replies; an idle session is not automatically awakened. The skill keeps installation/configuration details, command syntax and the cooperation workflow together.

## Operate, upgrade and remove

```sh
codex-link daemon stop
codex-link configure --config /absolute/path/config.local.yaml
codex-link daemon start
# Upgrade: run the installer with a newer CODEX_LINK_VERSION.
# Remove: sh scripts/uninstall.sh
```

Data defaults to `~/Library/Application Support/codex-link/`: `config.json`, `state.db`, `files/`, `run.sock`, `daemon.log`. `CODEX_LINK_HOME` overrides it and must be short enough for a Unix socket. The LaunchAgent is `~/Library/LaunchAgents/io.jom.codex-link.plist`; only one managed daemon is supported per macOS user. LaunchAgents start after login, not before login. Keychain must be available to the logged-in user.

Updates preserve state and keep the previous binary/skill. To roll back, stop the service, restore the `.previous` binary and matching skill, then start. Uninstall preserves history, file cache and Keychain credentials. Local history/cache/logs currently require manual cleanup while the daemon is stopped; do not remove SQLite files while it is running.

## Reliability and limits

- Outbox persistence survives restarts; successful Redis appends are atomically deduplicated within the retention window.
- Incoming storage, cursor advancement and receipt enqueueing share one SQLite transaction.
- Transport is at least once, with duplicate suppression. Task side effects are not guaranteed exactly once.
- Offline recovery is bounded by stream trimming, expiration, Redis persistence and OSS lifecycle rules. Configure Redis persistence/backups; never describe retention settings as an absolute no-loss guarantee.
- Signed registry entries persist for discovery. Confirmed peer names/keys are pinned locally for offline name lookup. Heartbeats are every 30 seconds; `seen_at` older than 90 seconds indicates an offline peer. Duplicate names require explicit IDs.
- STS refresh, automatic Codex wakeup, GUI, automatic directory extraction, cross-user inboxes and S3-compatible storage are not implemented in this release.
- No cloud service credentials ship with the project. Live two-Mac validation still requires your infrastructure and credentials.

## Development

```sh
go test ./...
go test -race ./...   # Requires a working C toolchain for Go's race runtime.
go vet ./...
go test ./internal/link -run '^$' -bench BenchmarkFileEncryption -benchmem
sh scripts/package.sh v0.1.0
```

Tests cover automatic key agreement, mutual approval/rejection, new-device isolation, encrypted envelopes, file truncation/tampering/limits, SQLite restart persistence, concurrent task claims, delivery receipts, two daemon task/result exchange and offline catch-up with an embedded Redis test server. OSS tests use a TLS HTTP fixture and signed SDK requests; this is not a substitute for live OSS permission validation. Production code uses pure-Go SQLite and builds with `CGO_ENABLED=0`.

GitHub Actions tests pushes/PRs; pushing a `v*` tag tests and publishes both Mac packages and checksums to Releases. See [architecture](docs/architecture.md), [verification](docs/verification.md), and [contributing](CONTRIBUTING.md).

## License

MIT, copyright 2026 jom-io. Dependencies retain their own licenses; see [third-party notices](THIRD_PARTY_NOTICES.md).
