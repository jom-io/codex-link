import test from 'node:test';
import assert from 'node:assert/strict';
import { createDeepSeekHost } from './deepseek-harness.mjs';

const cfg = { nativeSessionId: 'native-session', timeZone: 'Asia/Shanghai' };

test('DeepSeek process-native API preserves chat identity and uses queue admission idempotency', async () => {
  let status = 'idle';
  const controller = {
    resolveAgent: async id => ({ id, status }),
    prompt: async (request, signal) => {
      assert.equal(request.sessionId, 'native-session');
      assert.equal(request.mode, 'queue');
      assert.equal(request.requestId, 'agent-link-wake-event-id');
      assert.deepEqual(request.content, [{ type: 'text', text: 'fixed local nudge' }]);
      assert.equal(signal, undefined); return { accepted: true };
    },
  };
  const host = createDeepSeekHost(controller, cfg);
  assert.equal(await host.inspect('native-session'), 'idle');
  status = 'running'; assert.equal(await host.inspect('native-session'), 'running');
  status = 'awaiting-approval'; assert.equal(await host.inspect('native-session'), 'waiting_user');
  await host.submit('native-session', 'fixed local nudge', undefined, 'event-id');
});

test('DeepSeek adapter also accepts wrapped resolver results and propagates resolution errors', async () => {
  const controller = {
    resolveAgent: async id => ({ agent: { id, status: 'idle' } }),
    prompt: async () => ({ accepted: true }),
  };
  assert.equal(await createDeepSeekHost(controller, cfg).inspect('native-session'), 'idle');
  controller.resolveAgent = async () => ({ error: { code: 'not-found' } });
  await assert.rejects(() => createDeepSeekHost(controller, cfg).inspect('native-session'));
  controller.resolveAgent = async () => { throw new Error('Session not found'); };
  await assert.rejects(() => createDeepSeekHost(controller, cfg).inspect('native-session'), /Session not found/);
});

test('DeepSeek adapter refuses wrong native identity and unconfirmed admission', async () => {
  const host = createDeepSeekHost({ resolveAgent: async () => ({ id: 'wrong-chat', status: 'idle' }), prompt: async () => ({ accepted: false }) }, cfg);
  await assert.rejects(() => host.inspect('native-session'));
  await assert.rejects(() => host.submit('native-session', 'nudge', undefined, 'id'));
  assert.throws(() => createDeepSeekHost(null, cfg), { code: 'UNSUPPORTED_NATIVE_API' });
});
