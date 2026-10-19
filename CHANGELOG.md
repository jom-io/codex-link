# Changelog

## v0.2.0 (unreleased)

- Automatic shared-daemon local delivery, authenticated private IPv4 direct
  delivery and existing Redis/OSS fallback for both CLI and MCP. Signed endpoint
  discovery reuses Redis and local cache; no new discovery dependency.
- Immutable encrypted file staging, verified receiver cache before LAN ACK,
  lost-ACK fallback deduplication, durable upload retries and route diagnostics.

- Rename the product/CLI to agent-link, with a shared codex-link compatibility entry and generic harness Skill.
- Preserve protocol v2, identity, Keychain, LaunchAgent, database, history/cache and OSS/crypto namespaces for in-place mixed-version upgrades.
- Add neutral AGENT_LINK_* environment settings with legacy fallback and a documented Git/source upgrade and retained rollback snapshots.
- Add bounded MCP stdio tools: read-only by default, explicit messaging/task and file opt-ins, fixed per-process session ownership; no command executor or automatic wakeup.
- Allocate independent named worker sessions for separate chats sharing one MCP process, while retaining explicit owner resumption.
- Document Codex, Claude Code and DeepSeek Harness integration and extend compatibility/MCP tests.


## v0.1.3 — 2026-10-03

- Add worker new, worker pulse, workers and task status commands.
- Automatically send encrypted activity on claim/renew and completion; explicit pulses report work stages and blocking states.
- Derive stale/unresponsive worker status after 90/180 seconds, notify state changes without refreshing the worker heartbeat, and prevent old updates from overwriting newer activity.
- Display device heartbeat age and possibly_offline status in peers. Protocol v2 remains compatible; upgrade both ends for structured activity queries.
- Window heartbeat is explicit, not automatic Codex wakeup or remote process monitoring.

## v0.1.2 — 2026-10-03

- Clarify the live collaboration loop: acknowledge a claimed task immediately, run long processes asynchronously, poll the inbox between process checks and send periodic progress updates.
- Explain that the daemon continues receiving messages during local work, while an idle or occupied Codex turn cannot automatically read or act on them. Independent tasks can be handled in another window with a unique session alias.
- Expand the second-Mac guide with queueing and progress expectations during long-running work.

## v0.1.1 — 2026-10-03

- Add `codex-link task list` to show incoming task IDs, status, lease owner and expiration, with session/conversation filters.
- Make task claim errors identify missing IDs, completed messages, non-task messages and active lease owners. Error guidance explicitly says to use the task's `message.id`, not `reply_to`.
- Clarify that each concurrently working Codex window needs its own stable session alias; the alias is the lease owner and can renew its own lease.
- Update English/Chinese installation and task workflow documentation.

## v0.1.0 — 2026-10-03

- First public release with automatic device identity, mutually confirmed pairing, encrypted Redis messages and OSS file transfer.
