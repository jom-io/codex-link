import test from 'node:test';
import assert from 'node:assert/strict';
import net from 'node:net';
import { mkdtemp, chmod, rm, writeFile, readFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { once } from 'node:events';
import { codexState, createCodexHost, codexService, readCodexConfig, CodexSubmissions } from './codex-desktop.mjs';
import { withCodexChat, validateCodexSocket } from './codex-ipc.mjs';
import { NativeDispatcher } from './native-host.mjs';

const native = '01234567-1234-1234-1234-0123456789ab';
const config = dir => ({ enabled: true, cliPath: '/usr/local/bin/agent-link', session: 'office-codex-test',
  nativeSessionId: native, from: ['home-device'], statePath: join(dir, 'wake.json'), socketPath: join(dir, 'ipc.sock') });
const event = id => ({ type: 'wake', id, session: 'office-codex-test', native_session: native,
  messages: [{ id: 'exact-message-id', kind: 'task' }], prompt: 'UNTRUSTED REMOTE BODY' });

async function fixture(t, overrides = {}) {
  const dir = await mkdtemp(join(tmpdir(), 'agent-link-codex-test-'));
  const c = config(dir), calls = [];
  const state = { id: native, hostId: 'local', cwd: '/selected/workspace', resumeState: 'resumed',
    requests: [], threadRuntimeStatus: { type: 'idle' }, ...overrides };
  const server = net.createServer(socket => {
    let buffer = Buffer.alloc(0);
    const send = value => {
      const body = Buffer.from(JSON.stringify(value)), head = Buffer.alloc(4); head.writeUInt32LE(body.length);
      const frame = Buffer.concat([head, body]); socket.write(frame.subarray(0, 2)); socket.write(frame.subarray(2));
    };
    socket.on('data', chunk => {
      buffer = Buffer.concat([buffer, chunk]);
      while (buffer.length >= 4 && buffer.length >= buffer.readUInt32LE(0) + 4) {
        const size = buffer.readUInt32LE(0), m = JSON.parse(buffer.subarray(4, size + 4)); buffer = buffer.subarray(size + 4);
        if (m.type === 'request') {
          calls.push(m);
          // Native desktop conformance: this local owner cannot use the cloud
          // user-message-admission extension (observed in the real desktop).
          if (m.method === 'thread-follower-start-turn' && m.params.turnStart.context.useUserMessageAdmission) {
            send({ type: 'response', requestId: m.requestId, resultType: 'error',
              error: 'User-message admission requires a cloud app-server host' });
            continue;
          }
          send({ type: 'response', requestId: m.requestId, method: m.method, handledByClientId: 'real-owner', resultType: 'success',
            result: m.method === 'initialize' ? { clientId: 'observer' } : m.method === 'thread-owner-discovery'
              ? { supportsUntrustedAppInput: true } : { result: { turn: { id: 'accepted-turn', status: 'inProgress' } } } });
        } else if (m.type === 'broadcast' && m.params.following) {
          send({ type: 'broadcast', method: 'thread-stream-state-changed', sourceClientId: 'real-owner', version: state.version ?? 11,
            params: { hostId: 'local', conversationId: native, change: { type: 'snapshot', revision: 1, conversationState: state } } });
        }
      }
    });
  });
  server.listen(c.socketPath); await once(server, 'listening'); await chmod(c.socketPath, 0o600);
  t.after(async () => { server.close(); await once(server, 'close'); await rm(dir, { recursive: true, force: true }); });
  return { dir, c, calls, state };
}

test('Codex native framing resolves the actual owner and retains only selected-chat state', async t => {
  const f = await fixture(t, { turns: [{ text: 'PRIVATE TRANSCRIPT' }], currentPermissions: { marker: 'PRIVATE SETTINGS' } });
  const state = await withCodexChat(f.c.socketPath, native, AbortSignal.timeout(3000), async (_client, s) => s.state);
  assert.equal(state.id, native); assert.equal(codexState(state), 'idle');
  assert.doesNotMatch(JSON.stringify(state), /PRIVATE/);
  assert.deepEqual(f.calls.map(c => c.method), ['initialize', 'thread-owner-discovery']);
  assert.equal(f.calls[0].params.clientType, 'agent-link-wake');
});

test('Codex wake uses only fixed retrieval input, inherits settings and durably deduplicates', async t => {
  const f = await fixture(t), host = await createCodexHost(f.c), id = 'a'.repeat(32);
  const d = new NativeDispatcher(f.c, host);
  assert.equal((await d.dispatch(event(id))).status, 'accepted');
  const nativeCalls = f.calls.filter(c => c.method === 'thread-follower-start-turn');
  assert.equal(nativeCalls.length, 1);
  const call = nativeCalls[0];
  assert.equal(call.targetClientId, 'real-owner'); assert.equal(call.version, 2);
  assert.equal(call.params.conversationId, native);
  assert.deepEqual(Object.keys(call.params.turnStart.request).sort(), ['clientUserMessageId', 'input', 'threadId']);
  assert.deepEqual(call.params.turnStart.context, { inheritThreadSettings: true, useUserMessageAdmission: false });
  const prompt = call.params.turnStart.request.input[0].text;
  assert.match(prompt, /exact-message-id/); assert.doesNotMatch(prompt, /UNTRUSTED REMOTE BODY/);
  const restarted = await createCodexHost(f.c);
  await restarted.submit(native, prompt, undefined, id);
  assert.equal(f.calls.filter(c => c.method === 'thread-follower-start-turn').length, 1);
});

test('Codex busy, approvals, unresolved submissions and unknown states never submit', async t => {
  const f = await fixture(t);
  for (const status of [{ type: 'active', activeFlags: [] }, { type: 'active', activeFlags: ['waitingOnApproval'] }, { type: 'future-state' }]) {
    f.state.threadRuntimeStatus = status;
    const host = await createCodexHost(f.c), d = new NativeDispatcher(f.c, host);
    assert.ok(['busy', 'waiting_user'].includes((await d.dispatch(event('b'.repeat(32)))).status));
  }
  f.state.threadRuntimeStatus = { type: 'idle' }; f.state.requests = [{ id: 'approval' }];
  assert.equal(await (await createCodexHost(f.c)).inspect(native), 'waiting_user');
  f.state.requests = []; f.state.unconfirmedTurnSubmissions = [{ requestId: 'unknown' }];
  assert.equal(await (await createCodexHost(f.c)).inspect(native), 'waiting_user');
  assert.equal(f.calls.some(c => c.method === 'thread-follower-start-turn'), false);
});

test('Codex rechecks native busy state at submission time', async t => {
  const f = await fixture(t), host = await createCodexHost(f.c);
  assert.equal(await host.inspect(native), 'idle'); f.state.threadRuntimeStatus = { type: 'active' };
  await assert.rejects(host.submit(native, 'fixed local nudge', undefined, 'c'.repeat(32)), { code: 'NATIVE_BUSY' });
  assert.equal(host.journal.uncertain(), false);
  assert.equal(f.calls.some(c => c.method === 'thread-follower-start-turn'), false);
});

test('unknown native admission is quarantined across process restart without retry or UI fallback', async t => {
  const f = await fixture(t), id = 'd'.repeat(32);
  const chat = async (_socket, _chat, _signal, operation) => operation({ request: async () => { throw new Error('lost native reply'); } },
    { owner: 'real-owner', state: { resumeState: 'resumed', runtimeStatus: { type: 'idle' } } });
  const host = await createCodexHost(f.c, { chat });
  await assert.rejects(host.submit(native, 'fixed nudge', undefined, id));
  const restarted = await createCodexHost(f.c);
  assert.equal(await restarted.inspect(native), 'waiting_user');
  await assert.rejects(restarted.submit(native, 'fixed nudge', undefined, id), { code: 'NATIVE_WAITING_USER' });
  assert.equal(f.calls.length, 0);
});

test('an explicitly verified rejected intent can be retained without blocking other notifications', async t => {
  const f = await fixture(t), id = 'f'.repeat(32);
  const journal = new CodexSubmissions(f.c);
  await journal.record(id, 'intent');
  assert.equal((await createCodexHost(f.c)).journal.uncertain(), true);
  // This transition is a local operator decision after checking the exact
  // native error and null turn ID, never a timeout or an automatic reset.
  await journal.record(id, 'rejected');
  const restarted = await createCodexHost(f.c);
  assert.equal(restarted.journal.get(id), 'rejected');
  assert.equal(await restarted.inspect(native), 'idle');
  assert.equal(JSON.parse(await readFile(journal.path)).entries[0].id, id);
});

test('Codex wrong identity, private socket and protocol checks fail closed', async t => {
  const f = await fixture(t), host = await createCodexHost(f.c);
  f.state.id = 'different-chat'; await assert.rejects(host.check(AbortSignal.timeout(3000)));
  f.state.id = native; f.state.version = 12; await assert.rejects(host.check(AbortSignal.timeout(3000)));
  f.state.version = 11;
  await chmod(f.c.socketPath, 0o666); await assert.rejects(validateCodexSocket(f.c.socketPath));
  await chmod(f.c.socketPath, 0o600);
  await assert.rejects(host.inspect('other-chat'));
});

test('Codex configs and journals are private and reject changed bindings', async t => {
  const f = await fixture(t), path = join(f.dir, 'binding.json');
  await writeFile(path, JSON.stringify(f.c), { mode: 0o600 }); assert.equal((await readCodexConfig(path)).session, f.c.session);
  await chmod(path, 0o644); await assert.rejects(readCodexConfig(path));
  const journal = new CodexSubmissions(f.c); await journal.record('e'.repeat(32), 'accepted');
  assert.equal(JSON.parse(await readFile(journal.path)).entries.length, 1);
  await assert.rejects(new CodexSubmissions({ ...f.c, session: 'home-codex-other' }).load());
});

test('Codex service is per binding, quotes paths, has no crash restart loop or shell', () => {
  const c = config('/private/selected dir');
  const a = codexService(c, '/private/binding & chat.json', { home: '/Users/test', node: '/selected/node', adapter: '/selected/codex-desktop.mjs' });
  const b = codexService({ ...c, session: 'home-codex-unique' }, '/private/home.json');
  assert.notEqual(a.label, b.label); assert.match(a.plist, /binding &amp; chat.json/);
  assert.match(a.plist, /<key>KeepAlive<\/key><false\/>/); assert.doesNotMatch(a.plist, /\/bin\/sh|osascript|resume/);
});
