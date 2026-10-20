# Inbox wake adapters

Wake is opt-in. A local harness plugin owns one `agent-link wake watch` stdio
child and binds a worker alias to one actual native chat ID. CLI and MCP remain
available. Pairing, wire encryption, local/LAN/cloud routing, the database and
the existing daemon socket do not change. No additional network listener or
model credentials are needed.

The listener keeps a private cursor and queue. On its first start it begins at
the current tail; subsequent starts recover unread arrivals and deferred
notifications. Only messages addressed to the configured worker session and
allowed sending device IDs match. The default kinds are `task,result`;
`text,file` require explicit opt-in. Progress, heartbeat, receipt and pairing
events never wake a model. Optional conversation filters bound the workflow.
Active leases owned by another window and completed tasks are skipped.

The listener sends metadata and a fixed retrieval nudge, never message bodies,
attachments or commands from the sender. The model still reads the actual
message through agent-link, checks human authorization, claims the task and
reports its verified result. A wake acceptance is not model execution or task
completion. Worker heartbeats are not refreshed by the listener.

## DeepSeek Harness desktop

Use the installed harness's native session API through a local Cordis plugin.
The default configuration is disabled. Enable only a selected chat and retain
its existing worker session. The real native ID comes from `DSH_SESSION_ID` in
that chat's shell; it is not the agent-link worker alias.

After installing from a pinned Git commit, the optional adapters are copied to
`~/.local/share/agent-link/adapters` by the normal installer. Back up the existing
`~/.dsh/profiles/desktop/cordis.patch.yml`, then append an entry, preserving the
other plugins and MCP permission options:

```yaml
- insert:
    - id: agent-link-wake
      name: 'file:///absolute/path/.local/share/agent-link/adapters/deepseek-harness.mjs?revision=PINNED_COMMIT'
      config:
        enabled: true
        cliPath: /absolute/path/.local/bin/agent-link
        session: home-ds-UNIQUE_WORKER
        nativeSessionId: session-ACTUAL_NATIVE_CHAT_ID
        from: [CONFIRMED_OFFICE_DEVICE_ID, THIS_DEVICE_ID]
        conversations: [authorized-cooperation]
        kinds: [task, result]
```

Use the actual absolute paths and IDs. Include the source revision query when
upgrading/reloading the module; it also refreshes its local helper modules.
Check the installed version's profile composition and native API before editing
a live profile. Do not print full composed configurations or HTTP auth tokens.

DeepSeek Harness desktop 0.2.1-rc.2 does not watch external edits to
`cordis.patch.yml`. The first binding needs profile reconciliation, triggered by
a save through the application's settings editor or an explicitly approved app
restart. A successful YAML edit or CLI installation alone does not load the
plugin. Prefer saving the existing settings through the native interface, and
verify listener readiness before testing. Preserve other settings and active
chats; module hot reload after loading is separate from loading a new profile
entry.

The plugin uses in-process `sessionController` calls. It checks the bound
Agent's actual state instead of inferring idleness from an old pulse. New
notifications wait while that Agent runs. Native `mode: queue` also handles a
race with a newly started turn. A stable request ID lets the host deduplicate
admission. Native failures or uncertain timeouts do not trigger UI submission.

Wait for the listener-ready log, let the selected chat finish its turn, then
send a fresh non-secret task from the permitted device/session. Require a real
model claim/reply/completion without another human prompt. For same-device
acceptance use `--to self --to-session BOUND_WORKER` and check `route: local`.
A script probe is a transport test, not proof that a second real model replied.

Set the plugin entry `disabled: true` or `config.enabled: false` to stop its
listener. Pending notifications and shared history are retained. Disable the
binding before deliberately ending automated cooperation; stopping an individual
model turn does not itself uninstall a configured inbox listener.

## Codex desktop (macOS)

`adapters/codex-desktop.mjs` is a reusable local sidecar for an **existing**
Codex desktop chat. Each Mac and each chat has its own configuration, worker,
native thread UUID and private state path. `office-codex` and `home-codex` use
the same adapter; names alone do not register a chat or grant permission.
CLI and MCP messaging remain available without changing their permissions.

The sidecar connects to the current user's private `CODEX_HOME/ipc/ipc.sock`
(normally `~/.codex/ipc/ipc.sock`). It initializes as `agent-link-wake`, discovers
the selected chat's native owner, reads that owner's selected-chat snapshot,
and submits a fixed retrieval nudge through the owning window. It never serves
owner discovery, takes ownership, starts another app-server, runs `codex exec
resume`, inspects other chats, or changes a chat's model, workspace or approval
settings. A snapshot transcript is parsed transiently and discarded; only
minimal native identity/readiness fields are retained. No API key or public IP
is needed. This is a same-user desktop integration, not an access boundary.

**Compatibility:** this optional coordination protocol is an internal desktop
interface, separate from the documented app-server JSON-RPC protocol. The
initial read-only check passed on the desktop bundled with codex-cli
`0.163.0-alpha.10.2`: snapshot v11, owner/following v1, start v2. Other versions
must pass `check`; unknown versions, missing owners, approvals, unloaded chats
or unavailable applications defer notifications. Do not guess protocol versions
or change application files to force compatibility. The documented app-server
API alone does not imply access to an existing desktop-owned chat.

After pulling a committed source revision and running the normal installer,
create a **0600** JSON file in this user's private application data directory:

```json
{
  "enabled": true,
  "cliPath": "/Users/YOU/.local/bin/agent-link",
  "session": "office-codex-RETAINED_UNIQUE_WORKER",
  "nativeSessionId": "ACTUAL-CODEX-THREAD-UUID",
  "from": ["CONFIRMED_PEER_DEVICE_ID"],
  "conversations": ["authorized-cooperation"],
  "kinds": ["task", "result"],
  "statePath": "/Users/YOU/Library/Application Support/codex-link/wake/office-codex-native.json"
}
```

Obtain the native UUID from `CODEX_THREAD_ID` in the selected desktop chat,
confirm it against that actual chat, and retain the unique agent-link session.
Use an existing Node 22+ runtime (including the desktop's bundled Node), an
absolute adapter path and an absolute private config path:

```sh
"/absolute/path/to/node" "$HOME/.local/share/agent-link/adapters/codex-desktop.mjs" check --config "/absolute/private/codex-wake.json"
"/absolute/path/to/node" "$HOME/.local/share/agent-link/adapters/codex-desktop.mjs" install --config "/absolute/private/codex-wake.json"
agent-link wake status --state "/absolute/private/state-from-config.json"
```

`install` requires a successful native check and `enabled: true`, creates one
LaunchAgent labelled by a hash of this worker/chat binding, and starts only that
sidecar. It also starts at login. There is no crash-restart loop: a failed wake
listener requires diagnosis. For foreground use replace `install` with `run`.
To disable the installed binding and stop its own listener:

```sh
"/absolute/path/to/node" "$HOME/.local/share/agent-link/adapters/codex-desktop.mjs" stop --config "/absolute/private/codex-wake.json"
```

State, history and queued notices are retained. Run `install` to explicitly
enable/start it again. Changed worker/chat/filter bindings need separate state
files. Git upgrades should reinstall the shipped adapters, check compatibility,
and restart only that binding. No full desktop restart is necessary.

Local Codex chats use the owning window's normal `turn/start` path with
`useUserMessageAdmission: false`. That desktop version's user-message-admission
extension requires a cloud app-server host; enabling it on a local chat can
display a prompt and then fail without a model turn. A visible prompt alone
therefore does not establish a successful wake.

The shared wake queue waits while the native chat is active. Submission obtains
a fresh native snapshot too; a busy transition detected there defers the event.
Codex owns final user-message admission; a simultaneous human prompt after that
last check can be admitted as steering by Codex rather than a separate turn.
There is no atomic idle-only operation in this desktop interface, so this adapter
does not promise a race-free next-turn boundary. It never interrupts a turn or
answers an approval. Existing drafts and the native follow-up queue are not
edited. The native message ID is deterministic for each wake event.

A private `.codex-submissions.json` beside the wake state records intent before
native submission and acceptance after confirmation. If submission times out,
disconnects, or fails after intent is recorded, the binding defers subsequent
notifications instead of blindly retrying or switching to UI. `check` reports
`uncertain_admission: true`; inspect the exact chat and native message ID before
manually resolving that journal. Stop the binding first. Only if the exact
native message ID has a verified validation rejection, failed status and no
model turn ID may an operator back up the journal and mark that intent
`rejected`; retain the entry and the evidence. Timeouts, a missing visible reply
or uncertainty do not justify that transition. Do not delete an intent just
to make a test green. Admission, native listener readiness and real model completion must be
verified separately. Two-way automatic wake requires a working binding on both
receiving chats, followed by a fresh task/result without human input on either.

## Desktop fallback

The adapter offers an explicitly enabled macOS fallback only when the native
submission API is absent. It requires Accessibility/Automation permission, an
unlocked available desktop, an exact app bundle/window binding, one empty text
composer, a Send button, and a visible unique chat marker containing the native
chat ID. Rename the selected test chat to expose that marker if necessary.
Generic app/window titles alone are insufficient. Existing drafts, a busy chat,
ambiguous controls or approval dialogs defer the notification. It never clicks
an approval, changes permissions, uses the clipboard or executes remote text.

The native route is preferred. Do not enable desktop fallback just to recover
from a network error or an uncertain native acceptance. Actual desktop behavior
must be verified against the selected application's accessibility tree.

## Host protocol and limits

```sh
agent-link wake watch --session UNIQUE_WORKER --native-session REAL_CHAT_ID \
  --from CONFIRMED_DEVICE_ID,self --conversation authorized-cooperation
agent-link wake status --state /absolute/path/reported-by-ready.json
```

`wake watch` emits newline-delimited JSON: a `ready` frame followed by `wake`
frames with `id`, `session`, `native_session` and at most 20 metadata entries.
The local host writes `{ "id": "EXACT_EVENT_ID", "status": "accepted" }` to
stdin only after native admission. `busy`, `waiting_user` and `stopped` retain
the queue. Invalid acknowledgments are rejected. Three adapter failures stop
the listener with its queue intact; diagnose and explicitly restart it.

One exclusive state lock prevents duplicate listeners for a binding. State is
0600, and a changed device/chat/filter binding needs a separate state file.
Accepted notifications have a five-second cooldown and a limit of ten per five
minutes; additional messages stay queued and are batched. Native host request
deduplication closes the common lost-ack gap; exactly-once model execution or
external effects are not promised. The native host stores a bounded private
acceptance cache too. No automatic task ownership or permission changes occur.

Other harnesses can use `adapters/native-host.mjs` with their local `inspect`
and `submit` functions. Codex desktop uses the selected-chat sidecar above;
do not replace it with a separate CLI resume process.


## Handled inbox messages (rc.10+)

After this chat processes exact incoming result/text/file IDs during any active
turn, call `agent-link inbox ack ID [ID ...] --session RETAINED_WORKER` before
ending it. MCP uses `agent_link_inbox_ack` with the existing write opt-in and
retained session. Reads are still read-only. The local per-chat marker survives
restart; scans and pending-queue rechecks exclude it. Other chats, history,
cursors and leases are unchanged, and unfinished tasks cannot be acknowledged.
There is no remote acknowledgement message or reply loop. Already admitted
native prompts cannot be withdrawn; a short check-to-admission race remains.
