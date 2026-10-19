# Verification record / 验证记录

## Optional native inbox wake

Run `go test -race ./...` (use the local codex-go wrapper where installed) and
`node --test adapters/*.test.mjs`; the adapters use Node built-ins only. Tests
cover explicit native identity, sender/conversation filtering, pagination,
exclusive bindings, durable deferred queues, completed/other-owner task
filtering, lost/forged acknowledgments, cooldown/rate limits, fixed local prompts,
native request IDs, busy/approval deferral and UI fallback restrictions.

Real acceptance must bind the actual installed harness, wait for listener ready,
finish the current model turn, then send a fresh selected test task. Require a
genuine model claim/result without another human input. Verify same-device
messages report `route: local`; do not label a script sender as a second model.
Disable/re-enable the listener and verify no wake while disabled, unread backlog
recovery and no repeated native admission for the same request ID. A native API
ACK is admission, not proof of task execution. The desktop fallback also needs
actual selected-app accessibility verification when used.

### Real DeepSeek desktop acceptance (2026-10-20, Asia/Shanghai)

Both Macs installed rc.4 from committed Git source at `7c75adc`; the home
upgrade used a SHA-256-verified Git bundle when GitHub access failed. The home
desktop runs DeepSeek Harness 0.2.1-rc.2. Its installed Cordis resolver confirmed
that injection object keys are service names; the adapter now declares
`['sessionController']`. The native controller returns Agent directly, which
the adapter accepts along with wrapped resolver results.

- A fresh office task entered the bound chat's busy queue. After the current
  model turn ended, the process-native plugin submitted its fixed retrieval
  prompt. The real model claimed and completed the exact task without another
  human chat input; native chat ID, worker alias and nonce matched. Listener
  parent PID belonged to the actual DSH host, and accepted count increased to 1.
- A CLI test publisher on the home Mac sent a task to the same real DeepSeek
  chat. While that chat ran, the task stayed pending with status `busy`. After
  the outer turn ended, a new native prompt started the model, which completed
  the inner task. Both its task and result reported `route: local`; native
  accepted count advanced from 1 to 2 to 3. This publisher is not a second
  model or a real Codex session.
- The same CLI publisher and real DeepSeek chat exchanged a new non-secret
  247-byte file in both directions. Both file records reported `route: local`
  and matched the complete SHA-256. An inner task automatically started the
  receiving model's next turn. A first send had hung before producing a message
  ID; restarting the link daemon restored service. Two later sends took 0.030s
  and 0.017s. No pre-restart stack was captured, so that incident's exact cause
  remains unknown.

The rc.5 concurrency regression reproduces a related defect deterministically:
pause an authenticated incoming file body, then send, deliver and fetch another
file locally. The old global lock blocks this local transfer; per-message locks
allow it to finish while the remote body remains paused. Tests also cover a
canceled same-file waiter and release of inactive lock entries. Full Go
race/vet checks passed after this fix. This reproduction does not establish the
uncaptured home incident's exact cause.

Both Macs then installed rc.5 from Git commit `f94b70b`. The home Mac ran the
two concurrency regressions with the race detector, retained the same native
listener through the daemon upgrade, and completed a fresh office task in a new
automatic native turn. CLI and daemon reported rc.5, the actual chat/worker IDs
matched, and the listener remained enabled with no pending notices. A private
temporary copy of the same 247-byte sample sent locally in 0.010s and fetched
in 0.013s with the original digest. The real home Codex test was deferred after
the user confirmed its network problem. A reverse read-only test found no wake
binding on office; its active model polled that task normally, so bidirectional
automatic wake is not claimed.

An independent office path-access issue remains: a background daemon read of a
bundle under Documents blocked in the system `open` call, while an identical
private temporary copy sent immediately. The thread sample does not identify
the OS-level cause. No privacy permissions were broadened. The blocked test
client was stopped and the daemon restarted to discard that test request.
Use a private copy of only the selected file when diagnosing such a path issue;
do not mistake it for a proven transfer-lock deadlock. See the file-path note
in the command reference.

Local Go race/vet checks and nine Node adapter tests passed. Real Codex desktop
wake, live UI fallback and native plugin disable/re-enable recovery remain
unverified; queue recovery and stopped dispatch have automated coverage.
Do not count a standalone mock adapter process or an old state file as evidence
that the desktop plugin is active. A running desktop host is required for its
bound native wake listener.

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

The user authorized using the existing plaintext Redis endpoint. Local configuration was updated explicitly (`tls: false`, `allow_insecure: true`), preserving device identity. Live `doctor --files` passed Redis authentication, discovery/registration and OSS encrypted upload/download. Two isolated local daemon instances using the actual Redis service passed bilateral pairing, queued task delivery, task result return and offline-message recovery after restart; their test namespace was cleaned. This is a live service test on one Mac, not a physical two-Mac confirmation.

## v0.2 acceptance

Run Go tests/race/vet using the local policy wrapper when installed. MCP tests enforce read-only default, opt-in files, session spoof rejection, bounded input validation, init/notification/error framing and named-worker uniqueness. Test the installer in an isolated prefix with `AGENT_LINK_NO_SERVICE=1` so it cannot stop the user's real daemon. Verify both command aliases and snapshot preservation. On real upgrades retain device ID, pair and SQLite history, then verify mixed-version communication before upgrading the second Mac. Finally use the real DeepSeek Harness MCP client/model, not just a hand-written JSON probe, for an authorized task/result exchange.

The dated v0.1 records above are historical. On 2026-10-19 the v0.2 harness work
passed local test/race/vet and CI, both actual Mac source upgrades, preservation
comparisons, real DeepSeek desktop task exchange, and an explicitly authorized
247-byte MCP file roundtrip with independent exact-byte/SHA-256 comparison.
Temporary DeepSeek file permissions were then revoked and verified.

## Automatic routing acceptance

Use the source's directed tests on both Macs:

```sh
GOCACHE=/private/tmp/codex-go-build-cache ~/.codex/bin/codex-go test ./internal/link -run 'Test(LocalTask|DirectTask|DirectOnPrivate|LostDirect|DirectDiscovery|DirectRejects|StagedCloud)' -count=1 -v
```

On a host without that policy wrapper, use its existing Go executable and the
same shared GOCACHE. `TestDirectOnPrivateInterface` must report PASS to establish
private-interface coverage; SKIP is not a direct-network acceptance. These tests
exercise two isolated identities on one host, not two physical Macs on one LAN.
See [routing acceptance](routing.md#acceptance) for invariants and real-Mac checks.
