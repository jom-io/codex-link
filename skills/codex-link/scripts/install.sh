#!/bin/sh
set -eu
# Fetch the repository installer into a temporary file; do not pipe curl into a shell.
ref="${CODEX_LINK_INSTALL_REF:-master}"
case "$ref" in *[!a-zA-Z0-9._-]*|'') echo "Invalid installation ref" >&2; exit 1 ;; esac
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT HUP INT TERM
curl --fail --location --retry 3 --proto '=https' --tlsv1.2 "https://raw.githubusercontent.com/jom-io/codex-link/$ref/scripts/install.sh" -o "$tmp"
sh "$tmp" "$@"
