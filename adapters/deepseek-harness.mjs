// Cordis local plugin for DeepSeek Harness desktop. Helpers share the revision
// query so reloading an upgraded entry does not retain stale Node ESM helpers.
const revision = new URL(import.meta.url).search;
const { startNativeBridge, UnsupportedNativeAPI } = await import(new URL('./native-host.mjs' + revision, import.meta.url));
const { inspectMacOSChat, submitMacOSChat, validateUI } = await import(new URL('./macos-ui.mjs' + revision, import.meta.url));

const { startSessionManager } = await import(new URL('./session-host.mjs' + revision, import.meta.url));

export const name = 'agent-link-wake';
// Cordis 4 maps object keys to service names. A legacy { optional: [...] }
// declaration would wait forever for a service literally named "optional".
export const inject = ['sessionController', 'agents'];

export function createDeepSeekHost(controller, config, registry = controller?.ctx?.agents) {
  const nativeAvailable = typeof controller?.resolveAgent === 'function' && typeof controller?.prompt === 'function';
  if (!nativeAvailable && !config.desktopFallback?.enabled) throw new UnsupportedNativeAPI();
  if (config.desktopFallback?.enabled) validateUI(config.desktopFallback, config.nativeSessionId);
  return {
    async presence(signal) {
      signal?.throwIfAborted();
      // Never resolve/rehydrate a historical agent just to manufacture liveness.
      // Only a native in-memory collection can provide passive presence.
      if (typeof registry?.get !== 'function') return { state: 'unknown' };
      const agent = registry.get(config.nativeSessionId);
      if (!agent) return { state: 'missing' };
      if (agent.id !== config.nativeSessionId) return { state: 'unknown' };
      if (['running', 'idle', 'stopped'].includes(agent.status)) return { state: agent.status };
      if (agent.status === 'disposed') return { state: 'stopped' };
      return { state: 'waiting_user' };
    },
    async inspect(sessionId, signal) {
      if (!nativeAvailable) return inspectMacOSChat(config.desktopFallback, sessionId, signal);
      signal?.throwIfAborted();
      const result = await controller.resolveAgent(sessionId);
      signal?.throwIfAborted();
      if (!result || result.error) throw new Error('Bound DeepSeek chat could not be resolved');
      // SessionController returns Agent directly; older/internal resolvers may
      // wrap it. Keep the native identity check for either API shape.
      const agent = result.agent ?? result;
      if (!agent || agent.id !== sessionId) throw new Error('DeepSeek returned a different native chat');
      if (agent.status === 'running') return 'running';
      if (agent.status === 'idle') return 'idle';
      if (agent.status === 'disposed' || agent.status === 'stopped') return 'stopped';
      // Unknown versions or an approval-specific state are never treated as idle.
      return 'waiting_user';
    },
    async submit(sessionId, prompt, signal, eventId) {
      if (!nativeAvailable) throw new UnsupportedNativeAPI();
      try {
        const result = await controller.prompt({
          requestId: 'agent-link-wake-' + eventId,
          sessionId, mode: 'queue', content: [{ type: 'text', text: prompt }],
          clientTimeZone: config.timeZone ?? 'Asia/Shanghai',
        }, signal);
        if (result?.accepted !== true) throw new Error('DeepSeek did not accept the bound prompt');
      } catch (error) {
        if (error.code === 'session/agent-busy') {
          const busy = new Error('DeepSeek chat became busy'); busy.code = 'NATIVE_BUSY'; throw busy;
        }
        throw error;
      }
    },
    fallback: config.desktopFallback?.enabled
      ? (id, prompt, signal) => submitMacOSChat(config.desktopFallback, id, prompt, signal)
      : undefined,
  };
}

export function apply(ctx, config = {}) {
  if (config.enabled !== true) return;
  const logger = message => ctx.logger?.info?.(message);
  const bridge = startSessionManager(config, binding => createDeepSeekHost(ctx.sessionController, binding, ctx.agents), startNativeBridge, logger);
  // The host owns the child lifecycle. HMR/disable/unload terminates only this
  // plugin's listener and never cancels a model turn or another application.
  if (typeof ctx.effect === 'function') ctx.effect(() => () => bridge.dispose());
  else if (typeof ctx.on === 'function') ctx.on('dispose', () => bridge.dispose());
  else { bridge.dispose(); throw new Error('This Cordis version has no plugin disposal hook'); }
}

export default { name, inject, apply };
