import test from 'node:test';
import assert from 'node:assert/strict';
import { replaceLaunchAgent } from './launchd.mjs';
test('per-chat launchd replacement tolerates asynchronous bootout without repeated stop', async () => {
  const calls = []; let attempts = 0;
  await replaceLaunchAgent('gui/501/own-label', '/own.plist', (args) => { calls.push(args); return { status: args[0] === 'bootstrap' && ++attempts < 3 ? 5 : 0 }; }, async () => {});
  assert.equal(attempts, 3); assert.equal(calls.filter(a => a[0] === 'bootout').length, 1);
  assert.ok(calls.every(a => a[0] !== 'bootout' || a[1] === 'gui/501/own-label'));
  await assert.rejects(() => replaceLaunchAgent('gui/501/own-label', '/own.plist', () => ({ status: 5 }), async () => {}), /queue\/config retained/);
});
