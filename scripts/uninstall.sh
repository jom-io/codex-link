#!/bin/sh
set -eu
bin_dir="${CODEX_LINK_BIN_DIR:-$HOME/.local/bin}"
skill_dir="${CODEX_LINK_SKILL_DIR:-${CODEX_HOME:-$HOME/.codex}/skills/codex-link}"
if [ -x "$bin_dir/codex-link" ]; then "$bin_dir/codex-link" daemon uninstall; fi
rm -f "$bin_dir/codex-link" "$bin_dir/codex-link.previous"
if [ -d "$skill_dir" ]; then rm -rf "$skill_dir"; fi
printf 'Application removed. Local history, cached files and Keychain credentials are preserved.\n'
