# Commands and cooperation protocol

```sh
codex-link status --json
codex-link peers --json
codex-link pair list --json
codex-link send --to office-mac --kind task --session requester --conversation setup --text "Check whether Go is installed and return the version. Do not install anything."
codex-link inbox --after 0 --session worker --conversation setup --json
codex-link task list --session worker --conversation setup --json
codex-link task claim TASK_ID --session worker-home-window-a --lease 900
codex-link send --to office-mac --kind progress --session worker-home-window-a --to-session requester --conversation setup --reply-to TASK_ID --text "Claimed; starting work. I will keep checking messages while this runs."
codex-link send --to home-mac --kind progress --session worker --conversation setup --reply-to TASK_ID --text "Environment inspection in progress"
codex-link task complete TASK_ID --session worker --text "Go version verified: ..."
codex-link wait --after 12 --session requester --conversation setup --timeout 30 --json
codex-link file send --to office-mac --path /absolute/path/report.zip --conversation setup
codex-link file fetch FILE_MESSAGE_ID
codex-link get MESSAGE_ID
codex-link history --after 0 --limit 100 --json
```

Place message/task IDs before flags. `--after` is a local numeric SQLite sequence, not a Redis ID or the remote machine's sequence. Default `wait --after 0` returns matching saved incoming messages; pass an advanced cursor to avoid repeated reads. `wait --after -1` waits only for future messages and can skip existing messages, so use it only deliberately. `inbox`/`history` always sort ascending, with up to 100 records by default (maximum 1000).

`--session` on send identifies the sender; `--to-session` routes to a receiver alias. On inbox/wait, `--session` includes that alias plus the device's public messages. All windows retain full local access; session filters provide routing, not an authorization boundary. Replies to tasks route back to the sender's alias. No opaque Codex chat ID is required or inferred.

Task leases default to 15 minutes. `task list` shows full task IDs, status, lease owner and expiry. Claim the exact task `message.id`; `reply_to` only links related messages and is never a substitute for that ID. Use a different stable `--session` alias in each concurrently working Codex window, because the alias identifies the lease owner and the same alias is allowed to renew its lease. Claim again with that same window alias to renew. Completion requires a current lease. Another window may reclaim an expired lease; do not assume exactly-once execution. A task completion queues one result in the same transaction.

Messages have `id`, `protocol`, `from`, `to`, `kind`, optional conversation/session/reply fields, and timestamp. File messages also have encrypted OSS object metadata. Receipts are internal and hidden from inbox/history. Use `get` to inspect outgoing delivery status.

An online peer has a recent `seen_at` (heartbeat every 30 seconds; treat older than 90 seconds as offline). The registry retains previously registered devices so their names can still be resolved offline. Duplicate names require an explicit device ID.

First-contact text/tasks queue while both native dialogs await confirmation. `pair/list` returns public peer information, request ID, code and local/remote approval state. `pair accept ID --code CODE` and `pair reject ID --code CODE` are local explicit decisions, not remote commands. Never accept without human authorization. Pairing events are hidden from ordinary inbox/history. First file send must be retried after pairing; the initial attempt uploads no data.
