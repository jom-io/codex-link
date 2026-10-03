# Verification record / 验证记录

Automatic pairing revision included. Local development verification: 2026-10-03, macOS arm64, Go 1.27.1. The module targets Go 1.25+.

## Passed

- `CGO_ENABLED=0 go test ./... -count=1 -timeout=90s -v`: all functional and integration tests passed.
- `CGO_ENABLED=0 go vet ./...`: no findings.
- `gofmt` and shell syntax checks for install, uninstall, packaging and skill installer scripts.
- CLI `version` and `--help` smoke checks; source installer tested using isolated binary/skill directories.
- Initialization creates private device keys automatically in a mocked Keychain and preserves them during reconfiguration.
- Native macOS pairing dialog script compiled successfully with `osacompile` (no automatic clicking).
- YAML import, unknown-field rejection and secret separation from runtime configuration.
- Automatic Ed25519/X25519 device identities, equal pairwise key derivation, signed messages and signature tamper rejection.
- Both approvals required, mismatched confirmation-code rejection, rejected-peer blocking, isolation of an added third device, simultaneous-request convergence and expiry rejection.
- File round trips (empty, partial/full chunks), truncation/tamper detection and size enforcement.
- Signed OSS V4 upload/download through a local TLS HTTP fixture; plaintext is not stored in the fixture.
- SQLite restart persistence, duplicate suppression, one-winner concurrent task claiming, ownership checks and transactional completion.
- Redis publish retry deduplication, encrypted payloads, two-daemon task/result exchange and offline delivery after receiver restart using miniredis.

File encryption microbenchmark (1 MiB input, in-memory source, discarded output): approximately 1.15 GB/s, 0.91 ms/op, 1.25 MB cumulative allocations/op on this development machine. These values do not measure network throughput, disk IO, idle daemon RSS or peak memory and are not a cross-language comparison.

## Pending infrastructure-dependent checks

- Real two-Mac Redis/OSS transfer, Redis TLS/ACL behavior, production OSS RAM policy and STS expiry.
- LaunchAgent/Keychain initialization and actual native dialog clicks on both configured Macs.
- Idle daemon CPU/RSS and long-running transfer measurements using actual configured services.
- macOS notarization/signing (not supplied in this release).

The local host has no active C developer toolchain, so local `go test -race` was not run. GitHub CI [run 37127487971](https://github.com/jom-io/codex-link/actions/runs/37127487971) passed `go vet`, `go test -race`, shell checks and both Mac builds for commit `06d67f2`. The release workflow repeats race tests before publishing; the latest run remains the authority for each tag.

真实公网验证需用户提供配置，不应把模拟服务测试描述为云服务联调通过。Release 编译和分发结果以 GitHub Actions 运行及资产列表为准。

## Local configured infrastructure check (2026-10-03)

The user-provided configuration imported successfully into macOS Keychain. A live OSS encrypted upload/download probe passed content verification and requested deletion of its temporary object. Redis did not pass: TLS was enabled in configuration but the configured endpoint answered an unauthenticated plaintext Redis PING. No credentials were sent by the plaintext diagnostic. End-to-end live messaging remains pending correction of the transport configuration and a two-device test. The public v0.1.0 installer successfully installed and verified the Mac binary and skill in isolated local directories.
