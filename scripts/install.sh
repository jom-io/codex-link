#!/bin/sh
set -eu
# Download a published release, or explicitly build a checked-out source tree.
repo="jom-io/codex-link"
bin_dir="${CODEX_LINK_BIN_DIR:-$HOME/.local/bin}"
skill_dir="${CODEX_HOME:-$HOME/.codex}/skills/codex-link"
source_dir=""
if [ "${1:-}" = "--source" ]; then
  source_dir="${2:-.}"
elif [ "$#" -ne 0 ]; then
  echo "Usage: sh scripts/install.sh [--source /path/to/codex-link]" >&2
  exit 1
fi
case "$(uname -s)" in Darwin) ;; *) echo "This installer targets macOS." >&2; exit 1 ;; esac
case "$(uname -m)" in arm64) arch=arm64 ;; x86_64) arch=amd64 ;; *) echo "Unsupported Mac architecture" >&2; exit 1 ;; esac
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
mkdir -p "$tmp/pkg"
if [ -n "$source_dir" ]; then
  source_dir=$(cd "$source_dir" && pwd)
  command -v go >/dev/null 2>&1 || { echo "Source installation requires Go 1.25+. Use a Release package otherwise." >&2; exit 1; }
  (cd "$source_dir" && CGO_ENABLED=0 go build -buildvcs=false -trimpath -o "$tmp/pkg/codex-link" ./cmd/codex-link)
  mkdir -p "$tmp/pkg/skills"
  cp -R "$source_dir/skills/codex-link" "$tmp/pkg/skills/"
else
  version="${CODEX_LINK_VERSION:-latest}"
  if [ "$version" = latest ]; then
    base="https://github.com/$repo/releases/latest/download"
  else
    case "$version" in *[!a-zA-Z0-9._-]*|'') echo "Invalid version" >&2; exit 1 ;; esac
    base="https://github.com/$repo/releases/download/$version"
  fi
  asset="codex-link_darwin_${arch}.tar.gz"
  curl --fail --location --retry 3 --proto '=https' --tlsv1.2 "$base/$asset" -o "$tmp/$asset"
  curl --fail --location --retry 3 --proto '=https' --tlsv1.2 "$base/checksums.txt" -o "$tmp/checksums.txt"
  awk -v file="$asset" '$2 == file {print}' "$tmp/checksums.txt" > "$tmp/selected.txt"
  [ -s "$tmp/selected.txt" ] || { echo "Missing release checksum" >&2; exit 1; }
  (cd "$tmp" && shasum -a 256 -c selected.txt)
  tar -xzf "$tmp/$asset" -C "$tmp/pkg" codex-link skills/codex-link
fi
[ -f "$tmp/pkg/skills/codex-link/SKILL.md" ] || { echo "Incomplete package" >&2; exit 1; }
mkdir -p "$bin_dir" "$(dirname "$skill_dir")"
# Stop a managed daemon before replacing its executable; preserve credentials/data.
restart=0
if [ -x "$bin_dir/codex-link" ] && /bin/launchctl print "gui/$(id -u)/io.jom.codex-link" >/dev/null 2>&1; then
  "$bin_dir/codex-link" daemon stop
  restart=1
fi
if [ -f "$bin_dir/codex-link" ]; then cp "$bin_dir/codex-link" "$bin_dir/codex-link.previous"; fi
if [ -d "$skill_dir" ]; then
  backup="$skill_dir.previous"
  if [ -d "$backup" ]; then rm -rf "$backup"; fi
  mv "$skill_dir" "$backup"
fi
cp "$tmp/pkg/codex-link" "$bin_dir/.codex-link.new"
chmod 755 "$bin_dir/.codex-link.new"
mv "$bin_dir/.codex-link.new" "$bin_dir/codex-link"
cp -R "$tmp/pkg/skills/codex-link" "$skill_dir"
if [ "$restart" -eq 1 ]; then "$bin_dir/codex-link" daemon start; fi
printf 'Installed: %s/codex-link\nSkill: %s\n' "$bin_dir" "$skill_dir"
printf 'Add %s to PATH if needed. Next: codex-link init --config config.local.yaml\n' "$bin_dir"
