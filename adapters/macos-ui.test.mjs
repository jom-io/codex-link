import test from 'node:test';
import assert from 'node:assert/strict';
import { validateUI } from './macos-ui.mjs';

test('desktop fallback refuses generic window titles without an exact chat marker', () => {
  const good = { enabled: true, bundleId: 'com.example.Harness', windowTitle: 'Harness', chatMarker: 'Wake test session-native-42', sendButtonTitle: 'Send' };
  assert.doesNotThrow(() => validateUI(good, 'session-native-42'));
  for (const bad of [{ enabled: false }, { chatMarker: 'a different conversation' }, { bundleId: 'app; bad' }, { sendButtonTitle: '' }, { windowTitle: 'title\nscript' }]) assert.throws(() => validateUI({ ...good, ...bad }, 'session-native-42'));
});
