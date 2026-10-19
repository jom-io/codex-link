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
and `submit` functions. Do not bind Codex's desktop chat to a separate CLI
resume process without verifying native identity, ownership and busy state.
