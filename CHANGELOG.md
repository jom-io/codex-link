# Changelog

## v0.1.1 — 2026-10-03

- Add `codex-link task list` to show incoming task IDs, status, lease owner and expiration, with session/conversation filters.
- Make task claim errors identify missing IDs, completed messages, non-task messages and active lease owners. Error guidance explicitly says to use the task's `message.id`, not `reply_to`.
- Clarify that each concurrently working Codex window needs its own stable session alias; the alias is the lease owner and can renew its own lease.
- Update English/Chinese installation and task workflow documentation.

## v0.1.0 — 2026-10-03

- First public release with automatic device identity, mutually confirmed pairing, encrypted Redis messages and OSS file transfer.
