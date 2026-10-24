import test from 'node:test';
import assert from 'node:assert/strict';
import { startSessionManager } from './session-host.mjs';

test('one loaded host dynamically attaches distinct chats, retains legacy identity and never duplicates listeners', async () => {
  const endpoints = [{ id: 'one', local: true, harness: 'deepseek-harness', profile: 'default', wake: true, session: 'worker-one', native_session: 'native-one', state_path: '/private/one' }];
  const configs = [], disposed = [], calls = [];
  const config = { cliPath: '/cli', session: 'worker-one', nativeSessionId: 'native-one', statePath: '/private/one', from: ['office'], kinds: ['task', 'result'] };
  const call = async (_, args) => { calls.push(args); if (args[1] === 'list') return endpoints; return {}; };
  const manager = startSessionManager(config, binding => ({ inspect: async id => { assert.equal(id, binding.nativeSessionId); if (id === 'missing-native') throw new Error('missing'); } }), binding => { configs.push(binding); return { dispose() { disposed.push(binding.session); } }; }, () => {}, { call, interval: 100000 });
  await manager.ready;
  assert.equal(configs.length, 1); assert.equal(configs[0].session, 'worker-one'); assert.deepEqual(configs[0].from, ['office']); assert.equal(configs[0].registry, true);
  await manager.tick(); assert.equal(configs.length, 1);
  endpoints.push({ ...endpoints[0], id: 'two', session: 'worker-two', native_session: 'native-two' },
    { ...endpoints[0], id: 'three', session: 'worker-three', native_session: 'missing-native' },
    { ...endpoints[0], id: 'other-profile', profile: 'different' }, { ...endpoints[0], id: 'remote', local: false });
  await manager.tick(); assert.equal(configs.length, 2); assert.equal(configs[1].nativeSessionId, 'native-two'); assert.deepEqual(configs[1].from, []);
  assert.equal(calls.filter(a => a[1] === 'attach').length, 1);
  manager.dispose(); assert.deepEqual(disposed, ['worker-one', 'worker-two']);
});

test('manager removes only archived chat listeners, resumes same identities, and supports more than 64 live chats', async () => {
  const endpoints = Array.from({ length: 70 }, (_, i) => ({ id: `id-${i}`, local: true, harness: 'deepseek-harness', profile: 'default', wake: true, session: `worker-${i}`, native_session: `native-${i}`, status: 'online' }));
  const started = [], stopped = [];
  const manager = startSessionManager({ cliPath: '/cli' }, () => ({ inspect: async () => 'idle' }), ep => { started.push(ep.session); return { dispose() { stopped.push(ep.session); } }; }, () => {}, { call: async (_, args) => args[1] === 'list' ? endpoints : {}, interval: 100000 });
  try {
    await manager.ready; assert.equal(started.length, 70);
    endpoints[0].status = 'archived'; await manager.tick(); assert.deepEqual(stopped, ['worker-0']);
    endpoints[0].status = 'online'; await manager.tick(); assert.equal(started.at(-1), 'worker-0'); assert.equal(started.length, 71);
  } finally { manager.dispose(); }
});

test('manager passive observation never resolves an absent historical chat', async () => {
 const endpoints = [{ id: 'old', local: true, harness: 'deepseek-harness', profile: 'default', wake: true, session: 'old-worker', native_session: 'old-native' }];
 const reported = []; let inspected = 0, started = 0, observed = 0;
 const manager = startSessionManager({ cliPath: '/cli' }, () => ({ presence: async () => { observed++; return { state: 'missing' }; }, inspect: async () => inspected++ }), () => { started++; return { dispose() {} }; }, () => {}, { call: async (_, args) => { if (args[1] === 'list') return endpoints; if (args[1] === 'presence') reported.push(args); return {}; }, interval: 100000 });
 try { await manager.ready; await manager.tick(); assert.equal(inspected, 0); assert.equal(started, 0); assert.equal(observed, 1); assert.equal(reported[0].at(-1), 'missing'); } finally { manager.dispose(); }
});
