#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
version="${1:-dev}"
case "$version" in *[!a-zA-Z0-9._-]*|'') echo "Invalid version" >&2; exit 1 ;; esac
out="$root/dist"
mkdir -p "$out"
for arch in arm64 amd64; do
  stage=$(mktemp -d)
  (cd "$root" && CGO_ENABLED=0 GOOS=darwin GOARCH="$arch" go build -buildvcs=false -trimpath -ldflags "-s -w -X github.com/jom-io/codex-link/internal/link.Version=$version" -o "$stage/codex-link" ./cmd/codex-link)
  cp -R "$root/skills" "$stage/skills"
  cp -R "$root/scripts" "$stage/scripts"
  cp -R "$root/docs" "$stage/docs"
  cp -R "$root/third_party" "$stage/third_party"
  cp "$root/THIRD_PARTY_NOTICES.md" "$stage/"
  cp "$root/LICENSE" "$root/README.md" "$root/README.zh-CN.md" "$root/CHANGELOG.md" "$root/config.example.yaml" "$stage/"
  COPYFILE_DISABLE=1 tar -czf "$out/codex-link_darwin_${arch}.tar.gz" -C "$stage" codex-link skills scripts docs third_party THIRD_PARTY_NOTICES.md LICENSE README.md README.zh-CN.md CHANGELOG.md config.example.yaml
  rm -rf "$stage"
done
(cd "$out" && shasum -a 256 codex-link_darwin_arm64.tar.gz codex-link_darwin_amd64.tar.gz > checksums.txt)
printf 'Release artifacts: %s\n' "$out"
