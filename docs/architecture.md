# Architecture and protocol v2

A single Go binary provides a transient CLI and a per-user daemon. CLI calls use JSON over a 0600 Unix socket, with no public HTTP listener. A filesystem lock prevents duplicate daemons. SQLite uses WAL and transactional updates. All windows under one macOS user share the store/cache; session aliases route messages but are not a sandbox.

## Identity and first-contact pairing

Initialization automatically generates Ed25519 signing and X25519 exchange keys, stored only in Keychain alongside cloud credentials. Device ID is a SHA-256-derived identifier of the signing public key. Signed discovery advertisements bind the name, ID, exchange public key and heartbeat timestamp. The public Redis registry is discoverable without a shared encryption key.

1. First send discovers the target and saves a signed pairing request with both identities, a fresh nonce and ten-minute expiration.
2. Both daemons save the pending request and display a native local confirmation dialog. The six-digit short authentication code binds both identities and the nonce; humans should compare it on both physical devices.
3. Each human confirmation persists a local decision and queues a signed acceptance. A pairing becomes ready only after local approval AND the verified remote approval.
4. Pinned public keys and the transcript are stored locally. Each pair derives a unique AES key through X25519 and HKDF-SHA256, bound to workspace, pairing nonce and both identities. No encryption key is configured, transported in plaintext or manually copied.
5. Deferred messages send only after pairing. A new device needs its own approvals and cannot inherit existing trust. Rejecting/timeouts leave ordinary messages unsent.

Simultaneous requests converge on the lexicographically smaller request ID. A request does not authorize task execution. Public names are untrusted until verified through confirmation; self-signed discovery alone proves key possession, not a human's intended device. Comparing the displayed code and IDs is necessary to detect misdirection. Static pairwise exchange keys do not provide forward secrecy against later device-key compromise.

## Message lifecycle

Outgoing messages are validated and saved to the SQLite outbox. A Lua script atomically adds an encrypted, signed envelope to the recipient's Redis Stream, sets its dedup marker, and applies retention. A crash after publish retries the same ID. Pairing decisions are sent before ordinary messages, so a receiver sees the acceptance before the first encrypted message.

Receiver verifies the sender signature, pinned identity, readiness and AES-GCM authentication. It atomically stores the message, advances its Redis cursor and queues a receipt. A receipt only updates a message whose destination matches its sender. One Redis reader per device supplies all windows' local inboxes; consumer groups do not divide messages between windows.

Pairing events contain public metadata and signatures, while ordinary message bodies are encrypted. Invalid, expired or unauthenticated entries are skipped with generic logs to prevent poison messages blocking reception. Namespace: `cl:v2:{workspace-hash}:inbox:DEVICE_ID`, `sent:DEVICE_ID:MESSAGE_ID`, and `devices`. Recommended deployment is standalone Redis; cluster/sentinel discovery is not implemented.

## Tasks and files

Task claiming is a conditional SQLite update with an expiring lease. Completion validates ownership/lease, saves the result and queues a response in one transaction. After crashes, task-specific idempotency is required for exactly-once external effects.

Files use a domain-separated key derived from the confirmed pair. Each 64 KiB AES-GCM frame authenticates its object path and index; an authenticated empty terminal frame detects truncation. Encrypted message metadata contains plaintext size and SHA-256. Sender encrypts to a private temporary file and uploads with OSS V4 signatures. Receiver confines objects to its workspace/device prefix, decrypts to a temp file, verifies integrity, then atomically renames into a per-message cache directory. No automatic extraction or execution occurs. Operations are serialized to bound memory. OSS lifecycle rules clean orphaned objects.

## Lifecycle and boundaries

LaunchAgent runs after user login; Keychain must be available. Native confirmation uses macOS scripting additions, not an active Codex chat. `pairing_headless` disables dialogs and requires an explicit human-authorized local acceptance command. Connections retry; idle receiving uses blocking XREAD. Shutdown cancels requests, closes Redis, waits for workers and closes SQLite.

Cloud operators see names/public keys, routing, timestamps, object paths and sizes; they can deny service or replay ciphertext. Local history/cache rely on user permissions and disk security, not SQLite encryption. Configuration changes preserve identity; lost identity keys cannot be silently replaced. No idle Codex wakeup, automatic STS refresh or remote command executor is present.
