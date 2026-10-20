import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { NativeDispatcher, UnsupportedNativeAPI, validateBinding } from './native-host.mjs';

const config = () => ({ enabled: true, cliPath: '/usr/local/bin/agent-link', session: 'home-ds-test', nativeSessionId: 'real-chat-42', from: ['office'] });
const event = () => ({ type: 'wake', id: 'a'.repeat(32), session: 'home-ds-test', native_session: 'real-chat-42', messages: [{ id: 'message-id', kind: 'task' }], prompt: 'REMOTE UNTRUSTED BODY MUST NOT BE FORWARDED' });

test('native dispatch binds one actual chat, reconstructs fixed input and deduplicates', async () => {
  let submitted = 0;
  const host = { inspect: async id => { assert.equal(id, 'real-chat-42'); return 'idle'; }, submit: async (id, prompt) => { submitted++; assert.equal(id, 'real-chat-42'); assert.match(prompt, /message-id/); assert.doesNotMatch(prompt, /UNTRUSTED BODY/); assert.match(prompt, /inbox ack HANDLED_IDS --session home-ds-test/); assert.match(prompt, /never acknowledge unfinished tasks/); } };
  const d = new NativeDispatcher(config(), host);
  assert.equal((await d.dispatch({ ...event(), native_session: 'other-chat' })).status, 'error');
  assert.equal((await d.dispatch(event())).status, 'accepted');
  assert.equal((await d.dispatch(event())).status, 'accepted');
  assert.equal(submitted, 1);
  d.dispose(); assert.equal((await d.dispatch(event())).status, 'stopped');
});

test('running, approval and unknown states do not submit another turn', async () => {
  for (const [state, expected] of [['running', 'busy'], ['waiting_user', 'waiting_user'], ['unknown', 'waiting_user'], ['stopped', 'stopped']]) {
    const d = new NativeDispatcher(config(), { inspect: async () => state, submit: async () => assert.fail('unexpected native submission') });
    assert.equal((await d.dispatch(event())).status, expected);
  }
});

test('UI fallback is permitted only for absent native submission, never uncertain errors', async () => {
  let fallback = 0;
  for (const error of [new Error('request timed out'), new UnsupportedNativeAPI()]) {
    const d = new NativeDispatcher(config(), { inspect: async () => 'idle', submit: async () => { throw error; }, fallback: async () => fallback++ });
    assert.equal((await d.dispatch(event())).status, error.code === 'UNSUPPORTED_NATIVE_API' ? 'accepted' : 'error');
  }
  assert.equal(fallback, 1);
});

test('accepted native notification is not resubmitted after adapter restart', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'agent-link-wake-test-'));
  try {
    const file = join(dir, 'accepted.json');
    const a = new NativeDispatcher(config(), { inspect: async () => 'idle', submit: async () => {} });
    await a.loadCache(file); await a.dispatch(event());
    const b = new NativeDispatcher(config(), { inspect: async () => assert.fail('duplicate preflight'), submit: async () => assert.fail('duplicate submission') });
    await b.loadCache(file); assert.equal((await b.dispatch(event())).status, 'accepted');
  } finally { await rm(dir, { recursive: true, force: true }); }
});

test('binding requires explicit sender, chat and fixed local executable', () => {
  for (const bad of [{ from: [] }, { nativeSessionId: '' }, { cliPath: 'agent-link' }, { kinds: ['progress'] }]) assert.throws(() => validateBinding({ ...config(), ...bad }));
});
