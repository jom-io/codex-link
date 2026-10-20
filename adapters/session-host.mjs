// Host lifecycle for persisted local endpoints; no remote address is a native ID.
import { execFile as execFileCallback } from 'node:child_process';
import { promisify } from 'node:util';
import { readFile, writeFile, mkdir, chmod } from 'node:fs/promises';
import { join, dirname, resolve } from 'node:path';
import { homedir } from 'node:os';
import { createHash } from 'node:crypto';
import { pathToFileURL } from 'node:url';
const execFile = promisify(execFileCallback);

export async function callCLI(cliPath, args) {
  const { stdout } = await execFile(cliPath, args, { timeout: 15000, maxBuffer: 2 * 1024 * 1024 });
  return JSON.parse(stdout);
}

// One loaded plugin manages only this host profile's registered native chats.
// Polling metadata doesn't claim work or invent model activity. Registry rules
// are enforced by the daemon again before every notice, even for queued work.
export function startSessionManager(config, createHost, startBridge, logger = () => {}, { call = callCLI, interval = 5000 } = {}) {
  const bridges = new Map(); let disposed = false, running = false, imported = false;
  const profile = config.profile ?? 'default';
  async function tick() {
    if (disposed || running) return; running = true;
    try {
      if (!imported && config.session && config.nativeSessionId) {
        // Upgrade exactly the legacy chat. Never substitute another native chat.
        const args = ['session', 'attach', '--harness', 'deepseek-harness', '--profile', profile,
          '--native-session', config.nativeSessionId, '--session', config.session, '--wake'];
        if (config.statePath) args.push('--state-path', config.statePath);
        await call(config.cliPath, args); imported = true;
      }
      const endpoints = await call(config.cliPath, ['session', 'list']);
      if (!Array.isArray(endpoints)) throw new Error('Invalid local endpoint directory');
      const selected = endpoints.filter(e => e.local && e.harness === 'deepseek-harness' && e.profile === profile && e.wake);
      if (selected.length > 64) throw new Error('Too many registered chats for this host');
      for (const ep of selected) {
        if (disposed) break;
        if (bridges.has(ep.id)) continue;
        const legacy = config.session === ep.session && config.nativeSessionId === ep.native_session;
        const binding = { enabled: true, registry: true, cliPath: config.cliPath,
          session: ep.session, nativeSessionId: ep.native_session, statePath: ep.state_path,
          from: legacy ? (config.from ?? []) : [], conversations: legacy ? config.conversations : undefined,
          kinds: legacy ? config.kinds : undefined, timeZone: config.timeZone };
        const host = createHost(binding);
        try { await host.inspect(ep.native_session, AbortSignal.timeout(8000)); }
        catch { continue; } // unknown/missing native chats never fall back to another
        await call(config.cliPath, ['session', 'receiver', '--session', ep.session, '--native-session', ep.native_session, '--state', 'connected']);
        if (disposed) break;
        const bridge = startBridge(binding, host, logger); bridges.set(ep.id, bridge);
        bridge.done?.finally(() => { if (bridges.get(ep.id) === bridge) bridges.delete(ep.id); }).catch(() => {});
      }
    } catch { logger('Session receiver refresh deferred; existing chat identities and queues retained'); }
    finally { running = false; }
  }
  const timer = setInterval(tick, interval); const ready = tick();
  return { ready, tick, bridges, dispose() { disposed = true; clearInterval(timer); for (const b of bridges.values()) b.dispose(); bridges.clear(); } };
}

export async function attachCodex(ep, { cliPath = process.env.AGENT_LINK_CLI_PATH ?? join(homedir(), '.local', 'bin', 'agent-link'), home = process.env.AGENT_LINK_HOME ?? process.env.CODEX_LINK_HOME ?? join(homedir(), 'Library', 'Application Support', 'codex-link'), run = execFile } = {}) {
  if (!ep.local || ep.harness !== 'codex' || !ep.wake || !ep.native_session || !ep.session) throw new Error('Not an enabled local Codex endpoint');
  const { createCodexHost, codexService } = await import('./codex-desktop.mjs');
  // Reuse an imported queue and the old filters/journal; only add registry rules.
  const statePath = ep.state_path ?? join(home, 'wake', createHash('sha256').update(ep.session + '\0' + ep.native_session).digest('hex').slice(0, 32) + '.json');
  let config = { enabled: true, cliPath, session: ep.session, nativeSessionId: ep.native_session, from: [], statePath };
  const { readdir } = await import('node:fs/promises');
  for (const name of await readdir(join(home, 'wake')).catch(() => [])) {
    if (!name.endsWith('.json')) continue;
    try { const old = JSON.parse(await readFile(join(home, 'wake', name), 'utf8'));
      if (old.session === ep.session && old.nativeSessionId === ep.native_session && old.cliPath) { config = { ...old }; break; }
    } catch { /* state files aren't adapter configs */ }
  }
  config.registry = true; config.enabled = true; config.cliPath = cliPath;
  const host = await createCodexHost(config); const check = await host.check(AbortSignal.timeout(10000));
  if (check.uncertain_admission) throw new Error('Native admission needs review before registration');
  const path = join(home, 'wake', 'endpoint-' + ep.id + '.binding.json');
  const body = JSON.stringify(config, null, 2);
  const existing = await readFile(path, 'utf8').catch(() => null);
  // Same chat reopen doesn't restart its listener or replace queues. A missing
  // LaunchAgent is repaired without creating a second identity.
  const service = codexService(config, path);
  let alive = false;
  if (existing === body) { try { const result = await run('/bin/launchctl', ['print', 'gui/' + process.getuid() + '/' + service.label], { timeout: 5000 }); alive = /^\s*pid = [1-9][0-9]*\s*$/m.test(result.stdout); } catch {} }
  if (!alive) {
    await mkdir(dirname(path), { recursive: true, mode: 0o700 }); await writeFile(path, body, { mode: 0o600 }); await chmod(path, 0o600);
    await run(process.execPath, [new URL('./codex-desktop.mjs', import.meta.url).pathname, 'install', '--config', path], { timeout: 20000 });
  }
  await callCLI(cliPath, ['session', 'receiver', '--session', ep.session, '--native-session', ep.native_session, '--state', 'connected']);
  return { endpoint: ep.id, receiver: 'connected', restarted: !alive };
}

if (process.argv[1] && pathToFileURL(resolve(process.argv[1])).href === import.meta.url) {
  if (process.argv[2] !== 'attach-codex') throw new Error('Usage: session-host.mjs attach-codex < local-endpoint.json');
  let data = ''; for await (const chunk of process.stdin) { data += chunk; if (data.length > 16384) throw new Error('Oversized endpoint'); }
  attachCodex(JSON.parse(data)).then(r => console.log(JSON.stringify(r))).catch(e => { console.error(e.message); process.exitCode = 1; });
}
