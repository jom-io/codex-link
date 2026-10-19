# Automatic routing (v0.2.0-rc.3)

CLI and MCP remain independent harness adapters to the same Unix socket. Routing
is a daemon concern, not an MCP transport, model service or task executor.

## Selection and persistence

All sends retain a durable SQLite outbox. The same device ID selects local
delivery before any cloud operation. After a file is cached, the shared database
row becomes an inbox row with a fresh arrival sequence, so readers that already
advanced past the queued row cannot miss it. Its ID, session, conversation and task lease semantics
are unchanged. Local history contains one copy, not separate sender/receiver
rows for the same unique ID. This applies to task results and worker progress too.

Other destinations try confirmed LAN endpoints, then Redis. File sends first
create a private encrypted outbox snapshot bound to the existing object ID and
pairwise file key. The snapshot is retained across failures and daemon restarts;
it is removed after acknowledged direct delivery or successful cloud publication.
OSS is used only on fallback. Successful upload state is durable so a failed
Redis publication does not require uploading the same snapshot again.

The receive path verifies the original v2 signed/encrypted envelope. LAN files
use the existing framed encryption, terminal-frame truncation check, size and
SHA-256 validation. The receiver commits the cache and message before returning
a signed/encrypted receipt. Senders verify receipt identity and message ID;
HTTP 200 alone is insufficient. A lost receipt can cause cloud retry, using the
same ID: insertion is deduplicated and even an already-received message gets a
cloud receipt. Task ownership is not reset. LAN traffic never changes the Redis
cursor. File uploads run separately from small-message/heartbeat workers.

## Discovery and listener

The existing signed Peer and protocol v2 are unchanged for older clients. A
separate `direct` Redis hash contains domain-separated signed endpoint records
bound to workspace, device identity, timestamp and protocol capability. Only
already-confirmed pinned keys are accepted. Valid records are cached locally;
discovery refreshes through Redis when possible. The initial discovery requires
Redis. With cloud unavailable, an existing cached endpoint is usable for up to
seven days if the address remains reachable. IP changes without Redis can
temporarily prevent direct discovery; messages remain queued for retry/fallback.

`auto` starts an ephemeral IPv4 TCP data listener if a private IPv4 interface
exists. It exposes only POST `/agent-link/v1/deliver`, with bounded frames/body,
header/authentication deadlines and bounded concurrent receives. Destinations
must be numeric IPs on one of the local private subnets; public addresses, DNS
names, redirects and HTTP proxies are not accepted. Requests from outside those
subnets are rejected. All application bodies and files remain pairwise encrypted
and signed; there is no new TLS certificate service. Administrators still use
the 0600 Unix socket; no management or command execution route is exposed on LAN.

`routing: cloud` suppresses the listener/advertisement and LAN attempts; local
delivery still works. Missing configuration defaults to `auto` without rewriting
existing config files or credentials. Redis configuration remains required for
initialization and cloud fallback. IPv6-only networks, subnet isolation and
firewalls use fallback. Multicast discovery and NAT traversal are out of scope.

## Acceptance

- Local task/result and file round trips with unavailable Redis and no OSS,
  including source mutation and receiver restart.
- Two independent device identities over TCP, with encrypted task completion and
  multi-frame file delivery when neither cloud service is available.
- Production listener on a real private interface when available (two isolated
  daemons on one host; not a claim of two physical Macs sharing a LAN).
- Lost direct ACK followed by actual Redis and encrypted OSS fixture fallback:
  one task/file ID, durable receipt and verified file bytes.
- Tampered file, forged ACK, forged/foreign/stale endpoint and off-subnet/public
  destination rejection. Staged cloud retry survives restart without reupload.
- Both real Macs upgrade from the same Git commit, preserve identity/history,
  and verify actual cross-Mac messages/files and same-daemon harness exchange.
  Record whether physical LAN reachability exists; otherwise report cloud
  fallback honestly rather than claiming a two-Mac LAN test.

The API reports the selected path in `get`/inbox/history records as `route`.
`delivered` still means saved by the daemon, not read or executed. A shared-daemon
local task becomes `received` and can subsequently be `claimed`/`completed`.
Before rollback to an older daemon, finish pending file sends; older daemons do
not understand the new encrypted staging state. Existing history/cache remains
readable without a destructive database migration.
