// Explicitly configured fallback for a native API that is genuinely absent.
// No clipboard, remote script, shell, approval button, or arbitrary window is used.
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
const execute = promisify(execFile);

export function validateUI(config, nativeSessionId) {
  if (config?.enabled !== true || !/^[A-Za-z0-9.-]+$/.test(config.bundleId ?? '') || !config.windowTitle || !config.sendButtonTitle || !config.chatMarker?.includes(nativeSessionId)) {
    throw new Error('UI fallback needs explicit app/window/send-button binding and a visible marker containing the native chat ID');
  }
  for (const value of [config.windowTitle, config.sendButtonTitle, config.chatMarker]) if (typeof value !== 'string' || value.length > 512 || /[\r\n\0]/.test(value)) throw new Error('Invalid UI binding');
}

// Arguments stay arguments. Prompt text and window metadata are never compiled
// into AppleScript. A chat marker is deliberately required in addition to title:
// desktop apps often reuse the same window title for many different chats.
export const macOSUIScript = `
on run argv
  set appID to item 1 of argv
  set expectedWindow to item 2 of argv
  set expectedMarker to item 3 of argv
  set sendTitle to item 4 of argv
  set operation to item 5 of argv
  set promptText to item 6 of argv
  tell application "System Events"
    set matches to every application process whose bundle identifier is appID
    if (count matches) is not 1 then return "stopped"
    set targetProcess to item 1 of matches
    set matches to every window of targetProcess whose name is expectedWindow
    if (count matches) is not 1 then return "waiting_user"
    set targetWindow to item 1 of matches
    set allItems to entire contents of targetWindow
    if (count allItems) > 2500 then return "waiting_user"
    set markerCount to 0
    set inputs to {}
    set sendButtons to {}
    repeat with anItem in allItems
      try
        set itemRole to role of anItem
        if itemRole is "AXSheet" or itemRole is "AXDialog" then return "waiting_user"
        set itemName to ""
        set itemValue to ""
        try
          set itemName to name of anItem
        end try
        try
          set itemValue to value of anItem
        end try
        if itemRole is "AXStaticText" and (itemName is expectedMarker or itemValue is expectedMarker) then set markerCount to markerCount + 1
        if itemRole is "AXButton" then
          if itemName is in {"Stop", "Stop generating", "停止", "停止生成"} then
            if enabled of anItem then return "running"
          end if
          if itemName is sendTitle then set end of sendButtons to contents of anItem
        end if
        if itemRole is "AXTextArea" then set end of inputs to contents of anItem
      end try
    end repeat
    if markerCount is not 1 or (count inputs) is not 1 or (count sendButtons) is not 1 then return "waiting_user"
    set targetInput to item 1 of inputs
    set sendButton to item 1 of sendButtons
    if (value of targetInput) is not "" then return "waiting_user"
    if operation is "inspect" then return "idle"
    set frontmost of targetProcess to true
    set focused of targetInput to true
    if not (frontmost of targetProcess) then return "waiting_user"
    -- Fixed local nudge only. Keystrokes make the application's input handler
    -- run; setting AXValue alone does not update every Electron/Tauri composer.
    keystroke promptText
    delay 0.2
    if (value of targetInput) is not promptText then return "waiting_user"
    if not (enabled of sendButton) then
      set value of targetInput to ""
      return "waiting_user"
    end if
    click sendButton
    return "accepted"
  end tell
end run
`;

async function call(config, nativeSessionId, operation, prompt, signal) {
  validateUI(config, nativeSessionId);
  if (process.platform !== 'darwin') throw new Error('Desktop fallback requires macOS');
  const args = ['-e', macOSUIScript, '--', config.bundleId, config.windowTitle, config.chatMarker, config.sendButtonTitle, operation, prompt];
  const { stdout } = await execute('/usr/bin/osascript', args, { signal, timeout: 8000, maxBuffer: 4096 });
  return stdout.trim();
}

export async function inspectMacOSChat(config, nativeSessionId, signal) {
  try {
    const state = await call(config, nativeSessionId, 'inspect', '', signal);
    return ['idle', 'running', 'stopped', 'waiting_user'].includes(state) ? state : 'waiting_user';
  } catch { return 'waiting_user'; } // includes locked/absent UI or missing TCC permissions
}

export async function submitMacOSChat(config, nativeSessionId, prompt, signal) {
  const result = await call(config, nativeSessionId, 'submit', prompt, signal);
  if (result !== 'accepted') throw new Error('Bound UI did not accept the notification');
}
