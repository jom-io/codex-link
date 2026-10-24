#!/bin/sh
set -eu
bin_dir="${AGENT_LINK_BIN_DIR:-${CODEX_LINK_BIN_DIR:-$HOME/.local/bin}}"
if [ -x "$bin_dir/agent-link" ]; then "$bin_dir/agent-link" daemon uninstall
elif [ -x "$bin_dir/codex-link" ]; then "$bin_dir/codex-link" daemon uninstall; fi
rm -f "$bin_dir/agent-link" "$bin_dir/codex-link"
printf 'Commands removed. Skills, rollback snapshots, local history, cached files and Keychain credentials are preserved.\n'
