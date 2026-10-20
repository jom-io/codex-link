import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, writeFile, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { startNativeBridge } from './native-host.mjs';

test('registry native bridge saves exact admission proof after host acceptance and before watcher ACK', async () => {
 const dir = await mkdtemp(join(tmpdir(), 'agent-link-native-admission-'));
 try {
  const cli = join(dir, 'cli'), state = join(dir, 'queue'), proof = join(dir, 'proof'), ack = join(dir, 'ack');
  const event = { type: 'wake', id: 'a'.repeat(32), session: 'retained-worker', native_session: 'exact-native-chat', messages: [{ id: 'probe-task', kind: 'task' }] };
  await writeFile(cli, `#!${process.execPath}\nconst fs=require('node:fs');\nif(process.argv[2]==='session'){fs.writeFileSync(${JSON.stringify(proof)},JSON.stringify(process.argv.slice(2)));console.log('{}');}else{console.log(JSON.stringify({type:'ready',session:'retained-worker',native_session:'exact-native-chat',state_path:${JSON.stringify(state)}}));setTimeout(()=>console.log(${JSON.stringify(JSON.stringify(event))}),25);process.stdin.once('data',data=>{if(!fs.existsSync(${JSON.stringify(proof)}))process.exit(9);fs.writeFileSync(${JSON.stringify(ack)},data);process.exit(0);});}`, { mode: 0o700 });
  let submitted = 0;
  const bridge = startNativeBridge({ enabled: true, registry: true, cliPath: cli, statePath: state, session: event.session, nativeSessionId: event.native_session, from: [] }, { inspect: async () => 'idle', submit: async id => { assert.equal(id, event.native_session); submitted++; } });
  await bridge.done;
  assert.equal(submitted, 1);
  const args = JSON.parse(await readFile(proof, 'utf8'));
  assert.deepEqual(args, ['session', 'receiver', '--session', event.session, '--native-session', event.native_session, '--state', 'admitted', '--event', event.id, '--messages', 'probe-task']);
  assert.equal(JSON.parse(await readFile(ack, 'utf8')).status, 'accepted');
  bridge.dispose();
 } finally { await rm(dir, { recursive: true, force: true }); }
});
