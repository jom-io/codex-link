#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
version="${1:-dev}"
case "$version" in *[!a-zA-Z0-9._-]*|'') echo "Invalid version" >&2; exit 1 ;; esac
out="$root/dist"
mkdir -p "$out"
go_cmd="${AGENT_LINK_GO:-$HOME/.codex/bin/codex-go}"
if [ ! -x "$go_cmd" ]; then go_cmd=go; fi
# macOS policy uses one shared cache. Linux CI retains Go/setup-go's own
# shared cache rather than trying to create the macOS-only /private path.
if [ "$(uname -s)" = Darwin ]; then
  export GOCACHE="${GOCACHE:-/private/tmp/codex-go-build-cache}"
fi
for arch in arm64 amd64; do
  if [ "$(basename "$go_cmd")" = codex-go ]; then
    (cd "$root" && CGO_ENABLED=0 GOOS=darwin GOARCH="$arch" "$go_cmd" binary ./cmd/agent-link -- -buildvcs=false -trimpath -ldflags "-s -w -X github.com/jom-io/codex-link/internal/link.Version=$version")
    built=$(cd "$root" && GOOS=darwin GOARCH="$arch" "$go_cmd" output-path ./cmd/agent-link)
  else
    mkdir -p "$out/build/darwin_$arch"
    built="$out/build/darwin_$arch/agent-link"
    (cd "$root" && CGO_ENABLED=0 GOOS=darwin GOARCH="$arch" "$go_cmd" build -buildvcs=false -trimpath -ldflags "-s -w -X github.com/jom-io/codex-link/internal/link.Version=$version" -o "$built" ./cmd/agent-link)
  fi
  stage=$(mktemp -d)
  cp "$built" "$stage/agent-link"
  cp "$built" "$stage/codex-link"
  for dir in skills scripts docs third_party; do cp -R "$root/$dir" "$stage/$dir"; done
  for file in THIRD_PARTY_NOTICES.md LICENSE README.md README.zh-CN.md CHANGELOG.md config.example.yaml; do cp "$root/$file" "$stage/"; done
  COPYFILE_DISABLE=1 tar -czf "$out/agent-link_darwin_${arch}.tar.gz" -C "$stage" agent-link codex-link skills scripts docs third_party THIRD_PARTY_NOTICES.md LICENSE README.md README.zh-CN.md CHANGELOG.md config.example.yaml
  # Old release URLs and upgrade scripts may continue selecting the legacy name.
  cp "$out/agent-link_darwin_${arch}.tar.gz" "$out/codex-link_darwin_${arch}.tar.gz"
  rm -rf "$stage"
done
(cd "$out" && shasum -a 256 agent-link_darwin_arm64.tar.gz agent-link_darwin_amd64.tar.gz codex-link_darwin_arm64.tar.gz codex-link_darwin_amd64.tar.gz > checksums.txt)
printf 'Release artifacts: %s\n' "$out"
