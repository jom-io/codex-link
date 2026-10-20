# Changelog

## v0.2.0-rc.10

- Add explicit per-chat handled-message acknowledgement through CLI and opt-in MCP.
  Preserve read-only reads, history, task ownership and separate file permissions.
- Filter handled IDs during scans and pending wake rechecks, including after
  restart, so receipts processed while busy do not cause a late duplicate wake.
- Reject foreign/outgoing messages and unfinished tasks atomically; keep
  already-admitted native prompts subject to the host’s existing semantics.

## v0.2.0-rc.7

- Use normal native turn submission for local Codex chats. The cloud-only
  user-message-admission extension caused a visible prompt followed by a native
  validation error without starting a model turn.
- Keep explicitly verified rejections in the admission journal. Unknown
  outcomes remain quarantined and are never automatically reset or retried.

## v0.2.0-rc.6

- Add an opt-in Codex desktop native adapter that discovers the selected chat's
  actual owning window over the existing private local coordination socket.
  Reuse it on any configured Mac with a compatible desktop protocol; no CLI
  resume process, new network listener or model credentials are introduced.
- Check native busy/approval state, retain the chat's existing settings, and
  quarantine uncertain prompt admission in a private per-binding journal.
- Add per-chat LaunchAgent installation, read-only compatibility checks,
  disable support and native-adapter regression tests.

## v0.2.0-rc.5

- Serialize file work by message ID instead of one global file lock, so a slow
  incoming transfer cannot block unrelated same-device sends or cached fetches.
- Cancel file-lock waiters with their request and release unused lock entries.

## v0.2.0-rc.4

- Add opt-in inbox wake notifications with explicit native-chat/sender binding,
  durable bounded queues, restart recovery, deduplication, cooldown and rate limits.
- Add a DeepSeek Harness Cordis adapter using process-native session state and
  idempotent queued prompts; stop its listener on plugin unload/disable.
- Add an explicitly bound macOS UI fallback for absent native APIs. Preserve
  drafts and approvals; uncertain native failures never switch to UI submission.
- Ship adapter resources with source/release installs without enabling them or
  changing existing identity, history, credentials, MCP permissions or routing.

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
