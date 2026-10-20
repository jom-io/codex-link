// launchd can acknowledge bootout before its registration has disappeared.
// Retry only our exact label; never touch another service or app process.
export async function replaceLaunchAgent(target, path, launchctl, pause = ms => new Promise(r => setTimeout(r, ms))) {
  launchctl(['bootout', target], false);
  launchctl(['enable', target]);
  let last;
  for (let attempt = 0; attempt < 12; attempt++) {
    const result = launchctl(['bootstrap', target.slice(0, target.lastIndexOf('/')), path], false);
    if (!result.error && result.status === 0) return;
    last = result;
    await pause(250);
  }
  throw new Error('Codex wake bootstrap did not complete; queue/config retained', { cause: last?.error });
}
