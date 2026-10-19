#!/bin/sh
set -eu
# agent-link keeps the existing daemon label, identity and storage for compatibility.
repo="${AGENT_LINK_REPOSITORY:-jom-io/codex-link}"
bin_dir="${AGENT_LINK_BIN_DIR:-${CODEX_LINK_BIN_DIR:-$HOME/.local/bin}}"
skill_dir="${AGENT_LINK_SKILL_DIR:-${CODEX_HOME:-$HOME/.codex}/skills/agent-link}"
legacy_skill_dir="${CODEX_LINK_SKILL_DIR:-${CODEX_HOME:-$HOME/.codex}/skills/codex-link}"
adapter_dir="${AGENT_LINK_ADAPTER_DIR:-$bin_dir/../share/agent-link/adapters}"
agent_name="${AGENT_LINK_AGENT:-}"
if [ -n "$agent_name" ]; then
  case "$agent_name" in *[!a-zA-Z0-9._-]*) echo "Invalid agent name" >&2; exit 1 ;; esac
  [ "${#agent_name}" -le 31 ] || { echo "Agent name must be at most 31 characters" >&2; exit 1; }
fi
source_dir=""
package_dir=""
case "${1:-}" in
  --source) source_dir="${2:?source directory required}" ;;
  --package) package_dir="${2:?verified extracted package directory required}" ;;
  '') ;;
  *) echo "Usage: sh scripts/install.sh [--source DIR | --package DIR]" >&2; exit 1 ;;
esac
[ "$#" -le 2 ] || { echo "Unexpected arguments" >&2; exit 1; }
case "$(uname -s)" in Darwin) ;; *) echo "This installer targets macOS." >&2; exit 1 ;; esac
case "$(uname -m)" in arm64) arch=arm64 ;; x86_64) arch=amd64 ;; *) echo "Unsupported Mac architecture" >&2; exit 1 ;; esac
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
mkdir -p "$tmp/pkg"
if [ -n "$source_dir" ]; then
  source_dir=$(cd "$source_dir" && pwd)
  go_cmd="${AGENT_LINK_GO:-$HOME/.codex/bin/codex-go}"
  if [ ! -x "$go_cmd" ]; then go_cmd=go; fi
  version="${AGENT_LINK_VERSION:-dev}"
  case "$version" in *[!a-zA-Z0-9._-]*|'') echo "Invalid version" >&2; exit 1 ;; esac
  export GOCACHE="${GOCACHE:-/private/tmp/codex-go-build-cache}"
  if [ "$(basename "$go_cmd")" = codex-go ]; then
    (cd "$source_dir" && CGO_ENABLED=0 "$go_cmd" binary ./cmd/agent-link -- -buildvcs=false -trimpath -ldflags "-s -w -X github.com/jom-io/codex-link/internal/link.Version=$version")
    built=$(cd "$source_dir" && "$go_cmd" output-path ./cmd/agent-link)
  else
    command -v "$go_cmd" >/dev/null 2>&1 || { echo "Source installation requires Go 1.25+. Use a Release package otherwise." >&2; exit 1; }
    mkdir -p "$source_dir/dist/build/darwin_$arch"
    built="$source_dir/dist/build/darwin_$arch/agent-link"
    (cd "$source_dir" && CGO_ENABLED=0 "$go_cmd" build -buildvcs=false -trimpath -ldflags "-s -w -X github.com/jom-io/codex-link/internal/link.Version=$version" -o "$built" ./cmd/agent-link)
  fi
  cp "$built" "$tmp/pkg/agent-link"
  cp -R "$source_dir/skills" "$tmp/pkg/skills"
  if [ -d "$source_dir/adapters" ]; then cp -R "$source_dir/adapters" "$tmp/pkg/adapters"; fi
elif [ -n "$package_dir" ]; then
  package_dir=$(cd "$package_dir" && pwd)
  cp "$package_dir/agent-link" "$tmp/pkg/agent-link"
  cp -R "$package_dir/skills" "$tmp/pkg/skills"
  if [ -d "$package_dir/adapters" ]; then cp -R "$package_dir/adapters" "$tmp/pkg/adapters"; fi
else
  version="${AGENT_LINK_VERSION:-${CODEX_LINK_VERSION:-latest}}"
  if [ "$version" = latest ]; then base="https://github.com/$repo/releases/latest/download"
  else
    case "$version" in *[!a-zA-Z0-9._-]*|'') echo "Invalid version" >&2; exit 1 ;; esac
    base="https://github.com/$repo/releases/download/$version"
  fi
  asset="agent-link_darwin_${arch}.tar.gz"
  curl --fail --location --retry 3 --proto '=https' --tlsv1.2 "$base/$asset" -o "$tmp/$asset"
  curl --fail --location --retry 3 --proto '=https' --tlsv1.2 "$base/checksums.txt" -o "$tmp/checksums.txt"
  awk -v file="$asset" '$2 == file {print}' "$tmp/checksums.txt" > "$tmp/selected.txt"
  [ -s "$tmp/selected.txt" ] || { echo "Missing release checksum" >&2; exit 1; }
  (cd "$tmp" && shasum -a 256 -c selected.txt)
  tar -xzf "$tmp/$asset" -C "$tmp/pkg" agent-link skills
  if tar -tzf "$tmp/$asset" adapters >/dev/null 2>&1; then tar -xzf "$tmp/$asset" -C "$tmp/pkg" adapters; fi
fi
for skill in agent-link codex-link; do
  [ -f "$tmp/pkg/skills/$skill/SKILL.md" ] || { echo "Incomplete package: $skill" >&2; exit 1; }
done
[ -f "$tmp/pkg/agent-link" ] && [ ! -L "$tmp/pkg/agent-link" ] || exit 1
chmod 755 "$tmp/pkg/agent-link"
"$tmp/pkg/agent-link" version
mkdir -p "$bin_dir" "$(dirname "$skill_dir")" "$(dirname "$legacy_skill_dir")"
# Keep immutable rollback snapshots keyed by the old binary digest. Never delete
# existing skills, histories, credentials, databases or previous backups.
old="$bin_dir/agent-link"
if [ ! -x "$old" ]; then old="$bin_dir/codex-link"; fi
backup=""
if [ -f "$old" ]; then
  digest=$(shasum -a 256 "$old" | awk '{print $1}')
  backup="$bin_dir/.agent-link-backups/$digest"
  mkdir -p "$backup"
  for name in agent-link codex-link; do
    if [ -f "$bin_dir/$name" ] && [ ! -e "$backup/$name" ]; then cp -p "$bin_dir/$name" "$backup/$name"; fi
  done
  if [ -d "$skill_dir" ] && [ ! -e "$backup/agent-link-skill" ]; then cp -Rp "$skill_dir" "$backup/agent-link-skill"; fi
  if [ -d "$legacy_skill_dir" ] && [ ! -e "$backup/codex-link-skill" ]; then cp -Rp "$legacy_skill_dir" "$backup/codex-link-skill"; fi
  if [ -d "$adapter_dir" ] && [ ! -e "$backup/adapters" ]; then cp -Rp "$adapter_dir" "$backup/adapters"; fi
fi
restart=0
if [ "${AGENT_LINK_NO_SERVICE:-0}" != 1 ] && /bin/launchctl print "gui/$(id -u)/io.jom.codex-link" >/dev/null 2>&1; then
  [ -x "$old" ] || { echo "Managed daemon exists without an installed CLI; repair manually." >&2; exit 1; }
  "$old" daemon stop
  restart=1
fi
for name in agent-link codex-link; do
  cp "$tmp/pkg/agent-link" "$bin_dir/.$name.new"
  chmod 755 "$bin_dir/.$name.new"
  mv "$bin_dir/.$name.new" "$bin_dir/$name"
done
# Overlay shipped files; preserve any locally added resources and rollback copy.
mkdir -p "$skill_dir" "$legacy_skill_dir"
cp -R "$tmp/pkg/skills/agent-link/." "$skill_dir/"
cp -R "$tmp/pkg/skills/codex-link/." "$legacy_skill_dir/"
if [ -d "$tmp/pkg/adapters" ]; then
  mkdir -p "$adapter_dir"
  cp -R "$tmp/pkg/adapters/." "$adapter_dir/"
fi
if [ -n "$agent_name" ]; then
  for dir in "$skill_dir" "$legacy_skill_dir"; do
    mkdir -p "$dir/references"
    printf '# Local agent identity\n\nThis installed Skill belongs to `%s`. Create a window with `agent-link worker new --agent %s`, then retain its unique full session. Keep device identities unchanged. A separate harness may use a different agent name.\n' "$agent_name" "$agent_name" > "$dir/references/local-agent.md"
  done
fi
if [ "$restart" -eq 1 ]; then
  if ! "$bin_dir/agent-link" daemon start; then
    echo "Daemon restart failed. Rollback snapshot: $backup. Data was preserved." >&2
    exit 1
  fi
fi
printf 'Installed: %s/agent-link\nCompatibility CLI: %s/codex-link\nSkill: %s\nRollback snapshot: %s\n' "$bin_dir" "$bin_dir" "$skill_dir" "$backup"
printf 'Existing installations: do NOT reinitialize. Run agent-link status and peers.\nNew installations: agent-link init --config config.local.yaml; agent-link daemon start\n'
if [ -d "$tmp/pkg/adapters" ]; then printf 'Optional native wake adapters (not enabled): %s\n' "$adapter_dir"; fi
