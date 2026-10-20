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
