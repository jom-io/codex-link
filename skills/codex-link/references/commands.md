# Commands and cooperation protocol

```sh
agent-link status --json
agent-link peers --json
agent-link pair list --json
agent-link send --to office-mac --kind task --session requester --conversation setup --text "Check whether Go is installed and return the version. Do not install anything."
agent-link inbox --after 0 --session worker --conversation setup --json
agent-link task list --session worker --conversation setup --json
agent-link task claim TASK_ID --session worker-home-window-a --lease 900
agent-link send --to office-mac --kind progress --session worker-home-window-a --to-session requester --conversation setup --reply-to TASK_ID --text "Claimed; starting work. I will keep checking messages while this runs."
agent-link send --to home-mac --kind progress --session worker --conversation setup --reply-to TASK_ID --text "Environment inspection in progress"
agent-link task complete TASK_ID --session worker --text "Go version verified: ..."
agent-link wait --after 12 --session requester --conversation setup --timeout 30 --json
agent-link file send --to office-mac --path /absolute/path/report.zip --conversation setup
agent-link file fetch FILE_MESSAGE_ID
agent-link get MESSAGE_ID
# Only after this chat has processed these exact incoming messages:
agent-link inbox ack RESULT_ID FILE_ID --session worker
agent-link history --after 0 --limit 100 --json
```

Place message/task IDs before flags. `--after` is a local numeric SQLite sequence, not a Redis ID or the remote machine's sequence. Default `wait --after 0` returns matching saved incoming messages; pass an advanced cursor to avoid repeated reads. `wait --after -1` waits only for future messages and can skip existing messages, so use it only deliberately. `inbox`/`history` always sort ascending, with up to 100 records by default (maximum 1000).

`inbox ack` (rc.10+) records handled state locally for 1–32 exact incoming IDs and the receiving full worker. Duplicate IDs and repeated calls are safe; an invalid ID, foreign chat, outgoing record or unfinished task rejects the whole batch. Results, text and files may be acknowledged after processing; a task must first be completed by this owner. Reads remain read-only, history/cursors/leases are unchanged, and acknowledgement sends no network message. A live wake listener rechecks queued IDs before submitting. An already admitted native prompt cannot be retracted. MCP exposes `agent_link_inbox_ack` only with `--allow-write`, with `messages: ["ID"]` and the chat’s retained `session`; it grants no file access.

`--session` on send identifies the sender; `--to-session` routes to a receiver alias. On inbox/wait, `--session` includes that alias plus the device's public messages. All windows retain full local access; session filters provide routing, not an authorization boundary. Replies to tasks route back to the sender's alias. No opaque harness chat ID is required or inferred.

Task leases default to 15 minutes. `task list` shows full task IDs, status, lease owner and expiry. Claim the exact task `message.id`; `reply_to` only links related messages and is never a substitute for that ID. Use a different stable `--session` alias in each concurrently working agent window, because the alias identifies the lease owner and the same alias is allowed to renew its lease. Claim again with that same window alias to renew. Completion requires a current lease. Another window may reclaim an expired lease; do not assume exactly-once execution. A task completion queues one result in the same transaction.

Messages have `id`, `protocol`, `from`, `to`, `kind`, optional conversation/session/reply fields, and timestamp. File messages also have encrypted OSS object metadata. Receipts are internal and hidden from inbox/history. Use `get` to inspect outgoing delivery status.

An online peer has a recent `seen_at` (heartbeat every 30 seconds; treat older than 90 seconds as offline). The registry retains previously registered devices so their names can still be resolved offline. Duplicate names require an explicit device ID.

First-contact text/tasks queue while both native dialogs await confirmation. `pair/list` returns public peer information, request ID, code and local/remote approval state. `pair accept ID --code CODE` and `pair reject ID --code CODE` are local explicit decisions, not remote commands. Never accept without human authorization. Pairing events are hidden from ordinary inbox/history. First file send must be retried after pairing; the initial attempt uploads no data.

## macOS file-path diagnosis

The background daemon opens the selected source path. If a file send stalls
before returning an ID while status/messages remain responsive, inspect the
file read as well as transport state. A Documents path has been observed stuck
inside system `open` even though an identical temporary copy sent immediately;
the OS-level cause was not confirmed. Use a private local temporary copy of
only that user-selected file (directory 0700, file 0600), preserve the original,
and verify the copy's hash and eventual receipt. Do not copy adjacent files or
broaden privacy permissions as a workaround. Inspect history before retrying
an uncertain send. `--timeout` controls `wait`; file RPCs default to 30 minutes,
so use a bounded caller when diagnosing a stalled operation.


## Lifecycle (rc.11+)

```sh
agent-link session list
agent-link session list --all --after 0 --limit 100
agent-link session list --archived
agent-link session gc --session FULL_WORKER --dry-run
agent-link session gc --session FULL_WORKER --apply
agent-link session pin
agent-link session unpin
agent-link session detach
# In that same actual chat, resume the retained identity/scope:
agent-link session attach --wake
```

MCP: `agent_link_session_list` accepts view `active|all|archived`; `agent_link_session_gc_preview` is read-only, while `session_gc_apply`, `session_pin/unpin/detach` require existing write opt-in and an issued retained session. Presence is adapter-local, not a model tool. Preview/apply skips running/approval/uncertain native admission and unfinished tasks/messages/files. Explicit inbox processing uses the rc.10 exact `inbox ack` workflow; reads never acknowledge. Archive/restore preserve history, ownership and existing pause/wake scope.
