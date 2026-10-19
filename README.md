# agent-link

[简体中文](README.zh-CN.md) · [MIT License](LICENSE)

Encrypted messaging, file transfer and task coordination between agent harnesses on your Macs. Codex, Claude Code and DeepSeek Harness use the same per-user Go daemon through a JSON CLI or MCP stdio. Model credentials stay with the harness.

**v0.2 development:** renamed from codex-link. `codex-link` remains a compatible command. Both names share the existing identity, pairing, SQLite history/cache and daemon; no reinitialization is needed. This repository and Go module retain their original `jom-io/codex-link` location during migration. No published v0.2 release is implied by a development checkout.

## What it provides

- Device identities, mutual human-approved pairing and pairwise encrypted messages/files.
- Redis Streams, SQLite outbox/retries/deduplication, receipts and offline catch-up within retention limits.
- Session/conversation routing, task claims/renewable leases and transactional completion replies.
- Explicit worker pulses and service/worker liveness, without fabricating worker activity.
- Aliyun OSS encrypted files, bounded-memory transfer and SHA-256 verification.
- macOS Keychain, private Unix socket, LaunchAgent and generic harness Skill.
- MCP stdio tools, read-only by default; opt-in cooperation and file operations.

```text
Codex / Claude Code / DeepSeek Harness
                 | CLI or MCP stdio + shared Skill
           agent-link local daemon
                 | SQLite + Keychain
        Redis Streams + private Aliyun OSS
                 | encrypted pairwise transport
           agent-link peer daemon
                 | another active harness
```

The daemon transports data. It does not execute received commands or automatically awaken idle chats. `delivered` means saved by the peer daemon, not read or executed. Session aliases route messages and own task leases; they are not a sandbox.

## Upgrade from Git

Requires Go 1.25+. Use an existing checkout or clone the repository; choose the reviewed branch/tag and verify the same commit on both Macs:

```sh
git clone https://github.com/jom-io/codex-link.git agent-link
cd agent-link
git switch codex/agent-link-multi-harness
git rev-parse HEAD
AGENT_LINK_VERSION=v0.2.0-rc.1 sh scripts/install.sh --source "$PWD"
"$HOME/.local/bin/agent-link" version
"$HOME/.local/bin/codex-link" status
"$HOME/.local/bin/agent-link" peers
```

For subsequent upgrades use `git fetch origin` and `git merge --ff-only origin/codex/agent-link-multi-harness` in a clean checkout. Do not discard local changes. The installer uses `~/.codex/bin/codex-go` when present, or Go otherwise; `AGENT_LINK_GO` can select the executable. Builds use a shared Go cache and stable output paths. Existing managed link daemons are stopped only for replacement, then restarted. Other services are untouched. Validate device ID, paired peer and history after upgrading. Do **not** run `init` again on an existing installation.

Each installer keeps rollback binaries/skills in `$BIN_DIR/.agent-link-backups/<old-binary-sha256>/`. To roll back, stop the link daemon, restore the saved binaries and matching skills from that snapshot, and start the link daemon. History, cached files and Keychain entries are never removed by upgrade. On an installation/restart failure, inspect the reported error and use the retained snapshot; there is no automatic rollback after a partial install.

Release packages include both command names and both skills. When a v0.2 release exists, use its inspected `scripts/install.sh`; `AGENT_LINK_VERSION` pins the version. Existing `CODEX_LINK_VERSION`, `CODEX_LINK_BIN_DIR` and `CODEX_LINK_SKILL_DIR` installer settings are accepted. A verified extracted package can also be installed with `sh scripts/install.sh --package /absolute/package`.

## New installation

Copy `config.example.yaml` to the ignored `config.local.yaml`, fill credentials locally and protect it with mode 600. Never paste it into a chat. Both devices use the same workspace and OSS bucket/prefix, different device names, and independently generated identities.

```sh
agent-link init --config /absolute/config.local.yaml
agent-link doctor --files
agent-link daemon start
agent-link peers
```

Each Mac automatically creates Ed25519/X25519 identities in Keychain. First contact displays a six-digit code on both Macs; compare the codes and intended devices before confirming on each. New devices do not inherit trust. Files queued before pairing must be resent after approval. A received task is never permission to execute it.

Public Redis should use TLS. Explicitly opting into plaintext Redis still exposes the Redis password in transit, despite payload encryption. See [infrastructure](docs/infrastructure.md), [architecture/security boundaries](docs/architecture.md) and the [detailed setup guide](docs/installation.zh-CN.md).

## Harness integration

Read [harness setup](docs/harnesses.md) for Codex/Claude skills, Claude MCP registration and other harnesses. DeepSeek Harness supports the generic Skill and MCP client; use the version-specific profile overlay documented there. The selected model is independent of agent-link.

```sh
# Read-only tool adapter. The host launches this command as an MCP subprocess.
agent-link mcp --harness deepseek-harness
# Permit user-authorized messaging/tasks; add --allow-files for selected files.
agent-link mcp --harness claude-code --allow-write
```

Each MCP process gets a unique session; read it with `agent_link_context`. A reconnecting worker holding a lease can explicitly retain its session with `--session`, but separate active windows must never share one lease owner. No shell, daemon-management, credential-configuration or pairing-accept MCP tools are exposed. MCP uses newline-delimited JSON-RPC stdio; calls are serialized and inbox waits are bounded to 30 seconds. It has no HTTP listener.

## CLI cooperation

```sh
agent-link worker new
agent-link send --to home-mac --kind task --session UNIQUE_SESSION --conversation check --text "Read the tool version and report it; do not change files."
agent-link inbox --session UNIQUE_SESSION --conversation check --after 0
agent-link task claim TASK_MESSAGE_ID --session UNIQUE_SESSION --lease 900
agent-link worker pulse TASK_MESSAGE_ID --session UNIQUE_SESSION --stage checking
agent-link task complete TASK_MESSAGE_ID --session UNIQUE_SESSION --text "Verified result ..."
agent-link file send --to home-mac --path /absolute/selected/report --conversation check
agent-link file fetch FILE_MESSAGE_ID
```

Claim the task's own `message.id`, never `reply_to`. Immediately acknowledge a claim. Keep checking messages between asynchronous process polls; pulse actual progress at most 60 seconds apart and renew the lease before expiry. Do not generate detached heartbeat loops. Complete only when the task is verified. Commands emit JSON. Inbox cursors are local `seq` values; advance after processing and page through full results. Files are authenticated before a path is returned; they are never automatically extracted/executed. [Full commands](skills/agent-link/references/commands.md).

## Compatibility and limits

- Protocol v2, Redis namespace, crypto domain separation and OSS defaults are unchanged, allowing mixed v0.1.3/v0.2 peers.
- Data stays in `~/Library/Application Support/codex-link/`; Keychain service and LaunchAgent remain `io.jom.codex-link`. Only one managed link daemon per macOS user.
- `AGENT_LINK_HOME` overrides the data directory, with legacy `CODEX_LINK_HOME` fallback. An explicitly empty neutral value suppresses the legacy override. New and old credential environment prefixes are supported; no identity is regenerated during upgrade.
- LaunchAgents start after login and require accessible Keychain. No Windows/Linux installer or service-manager integration is added.
- At-least-once transport/deduplication does not guarantee exactly-once task effects. A crashed/expired lease needs task-specific idempotency. Stale activity does not prove a process stopped.
- Cloud operators can see names, public keys, routing, timings, sizes and object paths; local SQLite/cache are protected by user permissions, not database encryption. Static pairwise keys do not provide forward secrecy.
- Redis/OSS retention bounds offline recovery. STS refresh remains manual. No automatic task executor, idle-chat wakeup, GUI, archive extraction, cross-user inbox or new storage provider is added.

## Development

```sh
go test ./...
go test -race ./...
go vet ./...
sh scripts/package.sh v0.2.0-rc.1
```

On a machine with a Go policy wrapper, run Go commands through that wrapper (for example `~/.codex/bin/codex-go test ./...`). Package/source scripts detect it automatically and accept `AGENT_LINK_GO`. Package artifacts use stable `dist/` filenames for arm64/amd64, including legacy filenames. No model API client or additional Go dependency is introduced by MCP.

Tests cover pairing/encryption, leases/persistence, encrypted files, daemon exchange, environment compatibility and MCP lifecycle/permission/session boundaries. Two-Mac acceptance additionally verifies mixed-version delivery, preserved identity/history, and a real harness tool exchange. See [verification](docs/verification.md), [changelog](CHANGELOG.md) and [contributing](CONTRIBUTING.md).
