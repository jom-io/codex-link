// Reusable, opt-in native wake for an existing Codex desktop chat.
import { readFile, writeFile, rename, mkdir, lstat, open } from 'node:fs/promises';
import { dirname, join, isAbsolute, resolve } from 'node:path';
import { homedir } from 'node:os';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { createHash } from 'node:crypto';
import { spawnSync } from 'node:child_process';
import { startNativeBridge, validateBinding } from './native-host.mjs';
import { replaceLaunchAgent } from './launchd.mjs';
import { withCodexChat } from './codex-ipc.mjs';

export function codexState(state) {
  if (!state || state.resumeState !== 'resumed' || state.hasUnconfirmedSubmission) return 'waiting_user';
  const status = state.runtimeStatus;
  if (state.hasRequests || status?.activeFlags?.length) return 'waiting_user';
  if (status?.type === 'active') return 'running';
  return status?.type === 'idle' ? 'idle' : 'waiting_user';
}

export function validateCodexBinding(config) {
  validateBinding(config);
  if (!/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/i.test(config.nativeSessionId)) throw new Error('Bind the actual Codex thread UUID');
  if (!isAbsolute(config.statePath ?? '')) throw new Error('An explicit private wake statePath is required');
  if (config.socketPath != null && !isAbsolute(config.socketPath)) throw new Error('Codex socketPath must be absolute');
  if (config.desktopFallback?.enabled) throw new Error('This Codex adapter uses the native selected-chat API only');
}

async function privateFile(path, optional = false) {
  try {
    const s = await lstat(path);
    if (!s.isFile() || s.uid !== process.getuid?.() || (s.mode & 0o077)) throw new Error('Use a private regular file owned by this user');
    return s;
  } catch (error) { if (!(optional && error.code === 'ENOENT')) throw error; }
}

export class CodexSubmissions {
  constructor(config) {
    this.config = config; this.path = config.statePath + '.codex-submissions.json'; this.entries = [];
  }
  async load() {
    if (!await privateFile(this.path, true)) return;
    const s = JSON.parse(await readFile(this.path, 'utf8'));
    if (s.session !== this.config.session || s.nativeSessionId !== this.config.nativeSessionId ||
        !Array.isArray(s.entries) || s.entries.length > 128 || !s.entries.every(e => /^[a-f0-9]{32}$/.test(e.id) && ['intent', 'accepted', 'rejected'].includes(e.status))) {
      throw new Error('Invalid Codex submission journal or changed binding');
    }
    this.entries = s.entries;
  }
  get(id) { return this.entries.find(e => e.id === id)?.status; }
  uncertain() { return this.entries.some(e => e.status === 'intent'); }
  async record(id, status) {
    if (!/^[a-f0-9]{32}$/.test(id) || !['intent', 'accepted', 'rejected'].includes(status)) throw new Error('Invalid native admission entry');
    this.entries = [...this.entries.filter(e => e.id !== id), { id, status }].slice(-128);
    await mkdir(dirname(this.path), { recursive: true, mode: 0o700 });
    await privateFile(this.path, true); await privateFile(this.path + '.tmp', true);
    const body = JSON.stringify({ session: this.config.session, nativeSessionId: this.config.nativeSessionId, entries: this.entries });
    await writeFile(this.path + '.tmp', body, { mode: 0o600 }); await rename(this.path + '.tmp', this.path);
  }
}

function nativeDeferred(state) {
  const error = new Error('Codex chat is not ready for another turn');
  error.code = state === 'running' ? 'NATIVE_BUSY' : 'NATIVE_WAITING_USER'; return error;
}

export async function createCodexHost(config, { chat = withCodexChat } = {}) {
  validateCodexBinding(config);
  const socket = config.socketPath ?? join(process.env.CODEX_HOME ?? join(homedir(), '.codex'), 'ipc', 'ipc.sock');
  const journal = new CodexSubmissions(config); await journal.load();
  const selected = (signal, operation) => chat(socket, config.nativeSessionId, signal, operation);
  return {
    journal,
    async inspect(id, signal) {
      if (id !== config.nativeSessionId) throw new Error('Wrong Codex native chat');
      if (journal.uncertain()) return 'waiting_user';
      // An unavailable app or incompatible protocol defers; it never starts a
      // second Codex process, guesses idleness, or falls back to UI submission.
      try { return await selected(signal, (_client, { state }) => codexState(state)); }
      catch { return 'waiting_user'; }
    },
    async submit(id, prompt, signal, eventId) {
      if (id !== config.nativeSessionId) throw new Error('Wrong Codex native chat');
      if (journal.get(eventId) === 'accepted') return;
      if (journal.uncertain()) throw nativeDeferred('waiting_user');
      await selected(signal, async (client, { state, owner }) => {
        const readiness = codexState(state);
        if (readiness !== 'idle') throw nativeDeferred(readiness);
        // Check again in a fresh native snapshot, then submit through the actual
        // owning window. Inherit its model, sandbox, approvals and workspace.
        // A durable intent quarantines an ambiguous admission across restarts.
        await journal.record(eventId, 'intent'); signal?.throwIfAborted();
        const response = await client.request('thread-follower-start-turn', {
          conversationId: id,
          turnStart: {
            request: { threadId: id, clientUserMessageId: 'agent-link-wake-' + eventId,
              input: [{ type: 'text', text: prompt, text_elements: [] }] },
            // The user-message-admission extension is cloud-host-only on the
            // inspected desktop version. Local chats use normal turn/start.
            context: { inheritThreadSettings: true, useUserMessageAdmission: false },
          },
        }, 2, owner, signal, 8000);
        if (typeof response.result?.result?.turn?.id !== 'string') throw new Error('Codex did not confirm native turn admission');
        await journal.record(eventId, 'accepted');
      });
    },
    async check(signal) {
      return selected(signal, (_client, { state }) => ({
        native_session: state.id, host: 'local', state: codexState(state), protocol: 'codex-desktop-ipc-v11',
        uncertain_admission: journal.uncertain(),
      }));
    },
  };
}

export async function readCodexConfig(path) {
  if (!isAbsolute(path)) throw new Error('Use an absolute Codex wake config path');
  await privateFile(path);
  const config = JSON.parse(await readFile(path, 'utf8')); validateCodexBinding(config); return config;
}

const xml = value => String(value).replace(/[<>&"']/g, c => ({ '<': '&lt;', '>': '&gt;', '&': '&amp;', '"': '&quot;', "'": '&apos;' })[c]);
export function codexService(config, configPath, { home = homedir(), node = process.execPath, adapter = fileURLToPath(import.meta.url) } = {}) {
  const digest = createHash('sha256').update(config.session + '\0' + config.nativeSessionId).digest('hex').slice(0, 16);
  const label = 'io.jom.agent-link.wake.codex-' + digest;
  const log = config.statePath + '.host.log';
  const args = [node, adapter, 'run', '--config', configPath];
  return { label, path: join(home, 'Library', 'LaunchAgents', label + '.plist'), log,
    plist: `<?xml version="1.0" encoding="UTF-8"?>\n<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">\n<plist version="1.0"><dict><key>Label</key><string>${xml(label)}</string><key>ProgramArguments</key><array>${args.map(a => '<string>' + xml(a) + '</string>').join('')}</array><key>RunAtLoad</key><true/><key>KeepAlive</key><false/><key>Umask</key><integer>63</integer><key>StandardOutPath</key><string>${xml(log)}</string><key>StandardErrorPath</key><string>${xml(log)}</string></dict></plist>\n` };
}

function launchctl(args, required = true) {
  const r = spawnSync('/bin/launchctl', args, { encoding: 'utf8', timeout: 10000 });
  if (required && (r.error || r.status !== 0)) throw new Error('Codex wake LaunchAgent operation failed');
  return r;
}

export async function main(args) {
  if (args.length !== 3 || args[1] !== '--config' || !['check', 'run', 'install', 'stop'].includes(args[0])) {
    throw new Error('Usage: node codex-desktop.mjs check|run|install|stop --config /absolute/private/config.json');
  }
  const [command, , path] = args, config = await readCodexConfig(path);
  const service = codexService(config, path), target = 'gui/' + process.getuid();
  if (command === 'stop') {
    launchctl(['disable', target + '/' + service.label]);
    launchctl(['bootout', target + '/' + service.label], false);
    console.log(JSON.stringify({ stopped: true, label: service.label, history_retained: true })); return;
  }
  const host = await createCodexHost(config);
  if (command === 'check' || command === 'install') {
    const result = await host.check(AbortSignal.timeout(10000));
    if (command === 'check') { console.log(JSON.stringify(result)); return; }
    if (process.platform !== 'darwin' || config.enabled !== true) throw new Error('Installation requires macOS and enabled: true');
    if (result.uncertain_admission) throw new Error('Resolve the uncertain native admission before reinstalling');
    await mkdir(dirname(config.statePath), { recursive: true, mode: 0o700 });
    await mkdir(dirname(service.path), { recursive: true, mode: 0o700 });
    await privateFile(service.path, true); await privateFile(service.log, true);
    await writeFile(service.path, service.plist, { mode: 0o600 });
    const log = await open(service.log, 'a', 0o600); await log.close();
    await replaceLaunchAgent(target + '/' + service.label, service.path, launchctl);
    console.log(JSON.stringify({ installed: true, label: service.label, state_path: config.statePath, ...result })); return;
  }
  if (config.enabled !== true) throw new Error('Codex wake is disabled');
  process.umask(0o077);
  const bridge = startNativeBridge(config, host, message => console.error(new Date().toISOString() + ' ' + message));
  const stop = () => bridge.dispose();
  process.once('SIGTERM', stop); process.once('SIGINT', stop);
  await bridge.done;
}

if (process.argv[1] && pathToFileURL(resolve(process.argv[1])).href === import.meta.url) {
  main(process.argv.slice(2)).catch(() => { console.error('Codex wake failed; inspect the private binding, native compatibility and admission journal.'); process.exitCode = 1; });
}
