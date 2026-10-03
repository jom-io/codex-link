# Changelog

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
