# Verification record / 验证记录

Local development verification: 2026-10-03, macOS arm64, Go 1.27.1. The module targets Go 1.25+.

## Passed

- `CGO_ENABLED=0 go test ./... -count=1 -timeout=90s -v`: all functional and integration tests passed.
- `CGO_ENABLED=0 go vet ./...`: no findings.
- `gofmt` and shell syntax checks for install, uninstall, packaging and skill installer scripts.
- CLI `version` and `--help` smoke checks.
- YAML import, unknown-field rejection and secret separation from runtime configuration.
- AES-GCM envelope authentication, workspace/key mismatch rejection.
- File round trips (empty, partial/full chunks), truncation/tamper detection and size enforcement.
- Signed OSS V4 upload/download through a local TLS HTTP fixture; plaintext is not stored in the fixture.
- SQLite restart persistence, duplicate suppression, one-winner concurrent task claiming, ownership checks and transactional completion.
- Redis publish retry deduplication, encrypted payloads, two-daemon task/result exchange and offline delivery after receiver restart using miniredis.

File encryption microbenchmark (1 MiB input, in-memory source, discarded output): approximately 1.15 GB/s, 0.91 ms/op, 1.25 MB cumulative allocations/op on this development machine. These values do not measure network throughput, disk IO, idle daemon RSS or peak memory and are not a cross-language comparison.

## Pending infrastructure-dependent checks

- Real two-Mac Redis/OSS transfer, Redis TLS/ACL behavior, production OSS RAM policy and STS expiry.
- LaunchAgent/Keychain initialization on both configured Macs.
- Idle daemon CPU/RSS and long-running transfer measurements using actual configured services.
- macOS notarization/signing (not supplied in this release).

The local host has no active C developer toolchain, so local `go test -race` was not run. GitHub CI and Release workflows run `go test -race` on Ubuntu with its toolchain before packaging/publishing; inspect their actual status before declaring race verification complete.

真实公网验证需用户提供配置，不应把模拟服务测试描述为云服务联调通过。Release 编译和分发结果以 GitHub Actions 运行及资产列表为准。
