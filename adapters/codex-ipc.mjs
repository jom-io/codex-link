// Selected-chat client for Codex desktop's local coordination socket. This is
// an opt-in, versioned desktop protocol, not a new app-server or CLI resume.
import net from 'node:net';
import { randomUUID } from 'node:crypto';
import { lstat } from 'node:fs/promises';
import { dirname, isAbsolute } from 'node:path';

const MAX_FRAME = 32 * 1024 * 1024;
export const CODEX_VERSIONS = Object.freeze({ snapshot: 11, owner: 1, following: 1, start: 2 });

export async function validateCodexSocket(path) {
  if (!isAbsolute(path)) throw new Error('Codex socket must be an absolute local path');
  const [socket, parent] = await Promise.all([lstat(path), lstat(dirname(path))]);
  const uid = process.getuid?.();
  if (uid == null || !socket.isSocket() || socket.uid !== uid || (socket.mode & 0o077) ||
      !parent.isDirectory() || parent.uid !== uid || (parent.mode & 0o077)) {
    throw new Error('Codex socket and directory must be private and owned by this user');
  }
}

export class CodexIPC {
  constructor(path) {
    this.path = path; this.clientId = 'initializing'; this.socket = null;
    this.pending = new Map(); this.buffer = Buffer.alloc(0); this.broadcasts = new Set();
  }
  async connect(signal) {
    signal?.throwIfAborted();
    await validateCodexSocket(this.path);
    await new Promise((resolve, reject) => {
      const socket = net.createConnection(this.path); this.socket = socket;
      const timer = setTimeout(() => fail(new Error('Codex connection timed out')), 2500);
      const abort = () => fail(signal.reason ?? new Error('Codex connection aborted'));
      const cleanup = () => { clearTimeout(timer); signal?.removeEventListener('abort', abort); socket.off('error', fail); };
      const fail = error => { cleanup(); socket.destroy(); reject(error); };
      socket.once('error', fail);
      signal?.addEventListener('abort', abort, { once: true });
      socket.once('connect', () => {
        cleanup(); socket.on('data', chunk => this.read(chunk));
        socket.on('error', () => this.close()); socket.on('close', () => this.close()); resolve();
      });
      if (signal?.aborted) abort();
    });
    const response = await this.request('initialize', { clientType: 'agent-link-wake' }, 0, undefined, signal);
    if (typeof response.result?.clientId !== 'string' || !response.result.clientId) throw new Error('Invalid Codex initialization');
    this.clientId = response.result.clientId;
  }
  write(message) {
    if (!this.socket || this.socket.destroyed) throw new Error('Codex socket is disconnected');
    const body = Buffer.from(JSON.stringify(message));
    if (body.length > MAX_FRAME) throw new Error('Codex request exceeds frame limit');
    const header = Buffer.alloc(4); header.writeUInt32LE(body.length);
    this.socket.write(Buffer.concat([header, body]));
  }
  read(chunk) {
    try {
      this.buffer = Buffer.concat([this.buffer, chunk]);
      while (this.buffer.length >= 4) {
        const size = this.buffer.readUInt32LE(0);
        if (!size || size > MAX_FRAME) throw new Error('Invalid Codex frame size');
        if (this.buffer.length < size + 4) return;
        const message = JSON.parse(this.buffer.subarray(4, size + 4).toString('utf8'));
        this.buffer = this.buffer.subarray(size + 4);
        if (message.type === 'response') {
          const pending = this.pending.get(message.requestId);
          if (pending) {
            if (message.resultType === 'success' && message.method === pending.method) pending.resolve(message);
            else pending.reject(new Error('Codex native request was not accepted'));
          }
        } else if (message.type === 'broadcast') {
          for (const listener of this.broadcasts) listener(message);
        } else if (message.type === 'client-discovery-request') {
          // This sidecar never claims ownership or serves other chats.
          this.write({ type: 'client-discovery-response', requestId: message.requestId, response: { canHandle: false } });
        } else if (message.type === 'request') {
          this.write({ type: 'response', requestId: message.requestId, resultType: 'error', error: 'no-handler-for-request' });
        }
      }
    } catch { this.close(); }
  }
  request(method, params, version, targetClientId, signal, timeoutMs = 4000) {
    signal?.throwIfAborted();
    const requestId = randomUUID();
    return new Promise((resolve, reject) => {
      const cleanup = () => { clearTimeout(timer); signal?.removeEventListener('abort', abort); this.pending.delete(requestId); };
      const abort = () => { cleanup(); reject(signal.reason ?? new Error('Codex request aborted')); };
      const timer = setTimeout(() => { cleanup(); reject(new Error('Codex native request timed out')); }, timeoutMs);
      this.pending.set(requestId, {
        method, resolve: value => { cleanup(); resolve(value); }, reject: error => { cleanup(); reject(error); },
      });
      signal?.addEventListener('abort', abort, { once: true });
      try {
        this.write({ type: 'request', requestId, sourceClientId: this.clientId, version, method, params, targetClientId, timeoutMs });
      } catch (error) { cleanup(); reject(error); }
      if (signal?.aborted) abort();
    });
  }
  following(chat, owner, following) {
    this.write({ type: 'broadcast', method: 'thread-stream-following-changed', version: CODEX_VERSIONS.following,
      sourceClientId: this.clientId, targetClientIds: [owner], params: { conversationId: chat, hostId: 'local', following } });
  }
  async selectedChat(chat, signal) {
    const response = await this.request('thread-owner-discovery', { hostId: 'local', conversationId: chat }, CODEX_VERSIONS.owner, undefined, signal);
    const owner = response.handledByClientId;
    if (typeof owner !== 'string' || response.result?.supportsUntrustedAppInput !== true) throw new Error('No compatible native owner for this Codex chat');
    const state = await new Promise((resolve, reject) => {
      const cleanup = () => { clearTimeout(timer); this.broadcasts.delete(receive); signal?.removeEventListener('abort', abort); };
      const abort = () => { cleanup(); reject(signal.reason ?? new Error('Codex snapshot aborted')); };
      const timer = setTimeout(() => { cleanup(); reject(new Error('Codex selected-chat snapshot timed out')); }, 4000);
      const receive = message => {
        const p = message.params;
        if (message.method !== 'thread-stream-state-changed' || p?.conversationId !== chat ||
            p.hostId !== 'local' || message.sourceClientId !== owner || p.change?.type !== 'snapshot') return;
        cleanup();
        const s = p.change.conversationState;
        if (message.version !== CODEX_VERSIONS.snapshot || s?.id !== chat || s.hostId !== 'local') {
          reject(new Error('Unsupported Codex snapshot version or chat identity')); return;
        }
        // Do not retain transcripts, commands, attachments, or permission data.
        resolve({ id: s.id, hostId: s.hostId, cwd: s.cwd, resumeState: s.resumeState,
          runtimeStatus: s.threadRuntimeStatus, hasRequests: Array.isArray(s.requests) ? s.requests.length > 0 : !!s.requests && Object.keys(s.requests).length > 0,
          hasUnconfirmedSubmission: !!s.unconfirmedTurnSubmissions?.length });
      };
      this.broadcasts.add(receive); signal?.addEventListener('abort', abort, { once: true });
      try { this.following(chat, owner, true); } catch (error) { cleanup(); reject(error); }
      if (signal?.aborted) abort();
    });
    return { owner, state };
  }
  close() {
    const socket = this.socket; this.socket = null; socket?.destroy();
    for (const pending of [...this.pending.values()]) pending.reject(new Error('Codex connection closed'));
    this.buffer = Buffer.alloc(0); this.broadcasts.clear();
  }
}

export async function withCodexChat(socketPath, chat, signal, operation) {
  const client = new CodexIPC(socketPath); let owner;
  try {
    await client.connect(signal);
    const selected = await client.selectedChat(chat, signal); owner = selected.owner;
    return await operation(client, selected);
  } finally {
    if (owner && client.socket && !client.socket.destroyed) {
      try { client.following(chat, owner, false); } catch { /* disconnect removes the follower */ }
    }
    client.close();
  }
}
