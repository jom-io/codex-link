# agent-link architecture and protocol v2

A single Go binary provides a transient CLI and a per-user daemon. CLI/MCP administration calls use JSON over a 0600 Unix socket. Automatic routing can additionally open an authenticated, encrypted-data-only LAN endpoint on a private IPv4 subnet; it exposes no administration operations. A filesystem lock prevents duplicate daemons. SQLite uses WAL and transactional updates. All windows under one macOS user share the store/cache; session aliases route messages but are not a sandbox.

## Identity and first-contact pairing

Initialization automatically generates Ed25519 signing and X25519 exchange keys, stored only in Keychain alongside cloud credentials. Device ID is a SHA-256-derived identifier of the signing public key. Signed discovery advertisements bind the name, ID, exchange public key and heartbeat timestamp. The public Redis registry is discoverable without a shared encryption key.

1. First send discovers the target and saves a signed pairing request with both identities, a fresh nonce and ten-minute expiration.
2. Both daemons save the pending request and display a native local confirmation dialog. The six-digit short authentication code binds both identities and the nonce; humans should compare it on both physical devices.
3. Each human confirmation persists a local decision and queues a signed acceptance. A pairing becomes ready only after local approval AND the verified remote approval.
4. Pinned public keys and the transcript are stored locally. Each pair derives a unique AES key through X25519 and HKDF-SHA256, bound to workspace, pairing nonce and both identities. No encryption key is configured, transported in plaintext or manually copied.
5. Deferred messages send only after pairing. A new device needs its own approvals and cannot inherit existing trust. Rejecting/timeouts leave ordinary messages unsent.

Simultaneous requests converge on the lexicographically smaller request ID. A request does not authorize task execution. Public names are untrusted until verified through confirmation; self-signed discovery alone proves key possession, not a human's intended device. Comparing the displayed code and IDs is necessary to detect misdirection. Static pairwise exchange keys do not provide forward secrecy against later device-key compromise.

## Message lifecycle

Outgoing messages are validated and saved to the SQLite outbox. Same-daemon destinations become local inbox rows; confirmed reachable LAN peers receive the same signed/encrypted envelope directly. Otherwise a Lua script atomically adds it to the recipient's Redis Stream, sets its dedup marker, and applies retention. A crash after publish retries the same ID. Pairing decisions retain the cloud path and are sent before ordinary messages. A LAN retry never resets the Redis cursor. Lost direct receipts can fall back to Redis without duplicating tasks. See [routing](routing.md) for discovery, persistence and acknowledgment details.

Receiver verifies the sender signature, pinned identity, readiness and AES-GCM authentication. It atomically stores the message, advances its Redis cursor and queues a receipt. A receipt only updates a message whose destination matches its sender. One Redis reader per device supplies all windows' local inboxes; consumer groups do not divide messages between windows.

Pairing events contain public metadata and signatures, while ordinary message bodies are encrypted. Invalid, expired or unauthenticated entries are skipped with generic logs to prevent poison messages blocking reception. Namespace: `cl:v2:{workspace-hash}:inbox:DEVICE_ID`, `sent:DEVICE_ID:MESSAGE_ID`, and `devices`. Recommended deployment is standalone Redis; cluster/sentinel discovery is not implemented.

## Tasks and files

Task claiming is a conditional SQLite update with an expiring lease. Completion validates ownership/lease, saves the result and queues a response in one transaction. After crashes, task-specific idempotency is required for exactly-once external effects.

Files use a domain-separated key derived from the confirmed pair (same-daemon files use a local identity-derived key). Each 64 KiB AES-GCM frame authenticates its object path and index; an authenticated empty terminal frame detects truncation. Encrypted message metadata contains plaintext size and SHA-256. Sender stages an immutable encrypted snapshot; local/LAN delivery uses it without OSS. Cloud fallback uploads it with OSS V4 signatures, recording upload completion for retry. Receiver confines objects to its workspace/device prefix, decrypts to a temp file, verifies integrity, then atomically renames into a per-message cache directory. LAN delivery acknowledges only after the file and message persist. Cached fetches do not contact OSS. No automatic extraction or execution occurs. File sends are serialized independently of messages/heartbeats. OSS lifecycle rules clean orphaned cloud objects.

## Lifecycle and boundaries

LaunchAgent runs after user login; Keychain must be available. Native confirmation uses macOS scripting additions, not an active Codex chat. `pairing_headless` disables dialogs and requires an explicit human-authorized local acceptance command. Connections retry; idle receiving uses blocking XREAD. Shutdown cancels requests, closes Redis, waits for workers and closes SQLite.

Cloud operators see names/public keys, routing, timestamps, object paths and sizes; they can deny service or replay ciphertext. Local history/cache rely on user permissions and disk security, not SQLite encryption. Configuration changes preserve identity; lost identity keys cannot be silently replaced. An optional local host adapter can wake a deliberately bound native chat through the existing inbox API; it is separate from the transport daemon and adds no execution authorization. Automatic STS refresh and remote command execution remain outside this component.

## Harness adapters (v0.2)

The CLI lives in internal/cli and is shared by cmd/agent-link and the cmd/codex-link compatibility entry. The MCP stdio adapter forwards an explicit subset of operations to the same private Unix socket. It does not spawn an agent or accept pairings. Default tools are read-only; sending/claiming/completing tasks and file tools require separate startup opt-ins. The adapter provides a default session per process and an explicit worker_new tool for hosts sharing one MCP process across chats. Each chat retains its issued session; unissued session values are rejected. A restart can explicitly resume a prior owner via --session. The local harness still decides whether a received request is authorized. Existing protocol/crypto domains, data path and service identifiers deliberately retain their legacy spelling; the cosmetic rename cannot create a second daemon or identity.
