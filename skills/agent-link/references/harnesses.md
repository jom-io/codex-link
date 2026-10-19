# Harness integration

agent-link is a communication layer, not a model SDK or a remote command executor. A local Codex window, Claude Code session, or another harness running a DeepSeek model can use the same CLI and protocol. No model API keys belong in agent-link.

## Shell / Skills

Install the included `skills/agent-link` directory into the harness's supported skill directory. Codex's installer target is `${CODEX_HOME:-~/.codex}/skills/agent-link`. For Claude Code use `~/.claude/skills/agent-link` (copy the entire directory, including references). For another harness, first verify its actual project-instruction / skill discovery mechanism; do not assume it reads `AGENTS.md`, `CLAUDE.md` or a particular dot-directory. Add a reference to the skill to the relevant project instructions only when needed; preserve existing instructions.

Each concurrently active window creates and retains its own `agent-link worker new` session. Choose a conversation label for the job. Claim the exact task `message.id` before acting within human-authorized scope. Acknowledge promptly, check the inbox during long asynchronous operations, pulse actual progress, renew leases, then complete with evidence. Full commands: [commands](commands.md).

## MCP stdio

For a harness that supports MCP, register a local stdio server:

```json
{
  "mcpServers": {
    "agent-link": {
      "command": "/absolute/path/to/.local/bin/agent-link",
      "args": ["mcp", "--harness", "your-harness"]
    }
  }
}
```

Replace the absolute path and label. This is the conventional MCP JSON fragment, not a promise that every harness uses this config filename or wrapper. Merge it using the target's documented setup procedure; preserve unrelated servers, keys and settings. Do not copy credentials between machines.

The default server offers only context/status/peers/workers/get/task_status/inbox/history/task_list/wait. For user-authorized cooperation add `--allow-write` to enable send/task_claim/worker_pulse/task_complete. To transfer selected files also add `--allow-files`; file tools are disabled otherwise. There are no shell, configuration, daemon-management or pairing-accept tools. The host's tool approval policy still applies.

The server generates one unique session per process and exposes it through `agent_link_context`. For a reconnecting session holding a task lease, persist and pass that specific session via `--session`; do not assign one fixed session to every window. CLI and MCP can cooperate if the same actual window deliberately retains its lease owner. Harness labels are local metadata, not authorization and not model selection.

Call `agent_link_status`, then `agent_link_peers` and `agent_link_context`. Use `agent_link_send` only when messaging is authorized. On the receiving harness call `agent_link_inbox`, claim the exact task with `agent_link_task_claim`, acknowledge through send, and complete it. These tools map directly to the existing daemon API; a daemon upgrade is required only for core daemon changes, not for using the stdio adapter. This version supports newline-delimited JSON-RPC MCP stdio (2024-11-05, 2025-03-26, 2025-06-18 and 2025-11-25). Calls are serialized; `wait` is bounded to 30 seconds. In-flight calls are bounded by timeouts rather than cancelled by an MCP notification. There is no HTTP listener.

## Claude Code

Use its documented local MCP command, for example:

```sh
claude mcp add --transport stdio --scope local agent-link -- /absolute/path/agent-link mcp --harness claude-code --allow-write
claude mcp get agent-link
```

Register only in the user-selected project/scope. For file tools explicitly add `--allow-files`. Skill and MCP are alternatives; neither changes Claude Code's model or permission defaults.

## DeepSeek Harness desktop 0.2.1-rc.2

Verified against the bundled official runtime on macOS. It discovers user Skills in `~/.dsh/skills` / `~/.agents/skills` and project Skills in `.dsh/skills` / `.agents/skills`. Copy `skills/agent-link` to `~/.dsh/skills/agent-link` for this harness. Existing Skills and instructions remain intact.

For the desktop profile, append the following item to the existing YAML sequence in `~/.dsh/profiles/desktop/cordis.patch.yml`. Keep existing patches; do not replace `cordis.yml`, sessions, model or credential settings. The upstream bundled MCP template uses `insert`, not a generic `mcpServers` block:

```yaml
- insert:
    - id: agent-link-mcp
      name: "@deepseek-ai/dsh-mcp-client"
      config:
        serverName: agent_link
        transport: stdio
        command: /Users/dd/.local/bin/agent-link
        args: [mcp, --agent, home-ds, --harness, deepseek-harness, --allow-write]
        toolCallTimeoutMs: 90000
        failOnStartupError: true
```

Change the absolute path for another account. `--allow-write` enables the user-requested communication/task tools; omit it for read-only checks. Files remain disabled. The bundled HMR watches the profile patch file, so a correct patch can be live-reloaded without restarting the desktop app. Validate composition with the installed CLI's documented `--patch` / `--dump-config` flow before editing a running profile; inspect only the added entry, never dump secret-bearing full config into logs. Verify a new MCP subprocess, tool discovery and an actual model tool call. A standalone JSON-RPC probe alone does not prove the desktop loaded the plugin. This profile schema is version-specific and must be checked again for a different harness version.

## Other DeepSeek-backed harnesses

Identify the actual installed harness (binary, version, upstream and configured tool interface). If it supports shell tools, the generic skill/CLI is enough. If it supports MCP stdio, use the adapter above. If it supports neither, integration requires a harness-specific plugin; do not describe raw DeepSeek API compatibility as working harness integration. Keep its model/API settings untouched. Real acceptance requires the harness to call the tools or CLI and exchange an authorized task/result with another device; merely seeing a config entry is insufficient.

## Boundaries and verification

An incoming message never grants permission to execute. Session filters route messages but are not isolation. Pairing, private-key storage, encrypted files, worker leases and v2 transport remain independent of the selected harness. No automatic execution, idle-chat wakeup or exactly-once external effects are added. Use separate task IDs and session owners for separate workers. A task should report its actual harness and verified result during an integration test, without exposing API keys or private configuration.

## Agent names and concurrent windows

Use `agent-link worker new --agent office-codex` on office Codex, `--agent home-codex` on home Codex, and MCP `--agent home-ds --harness deepseek-harness` for home DeepSeek Harness. `AGENT_LINK_AGENT` is an optional process-local default. Agent names prefix unique worker sessions, e.g. `home-ds-<random-id>`; they do not rename devices or alter pairing. Reply routing still uses the full session returned by `worker new` / `agent_link_context`, so multiple windows with the same human-readable agent name retain separate lease ownership.
