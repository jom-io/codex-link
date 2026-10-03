# Architecture and protocol v1

A single Go binary provides a transient CLI and a per-user daemon. CLI calls use POST JSON over a 0600 Unix socket; no network HTTP listener is opened. A filesystem lock prevents duplicate daemons. SQLite uses WAL with one connection and transactional updates.

## Message lifecycle

1. Validate a request and resolve the peer name (or accept its stable device ID).
2. Persist an outgoing envelope to SQLite as pending.
3. Append an AES-256-GCM encrypted envelope to the recipient's Redis Stream; a Lua script atomically adds a dedup marker and applies retention.
4. Mark local status sent. A retry after a crash uses the same message ID.
5. Receiver authenticates/validates the envelope, then atomically stores it, advances its Redis cursor and queues a receipt.
6. Receipt updates delivery state only for a message whose destination matches the receipt sender.

There is one Redis reader per device, not one per chat. Local windows read the same durable inbox with their own sequence cursor. This deliberately avoids consumer groups distributing messages across local windows. Invalid/unauthenticated envelopes are skipped with a generic log so a poison entry cannot stop the inbox; unsupported protocol versions are also rejected.

Namespace: `cl:v1:{workspace-hash}:inbox:DEVICE_ID`, `sent:DEVICE_ID:MESSAGE_ID`, and `devices` registry. Redis hash tags keep publish-script keys in one cluster slot, but DB selection should be 0 for Redis Cluster and cluster client mode is not currently supported. Recommended deployment: standalone Redis or a compatible managed standalone endpoint.

## Cryptography and trust

A random shared workspace key derives separate message/file AES keys through domain-separated SHA-256. Messages bind workspace/protocol through associated data. File frames are 64 KiB with random nonces and authenticated object key/frame index, terminated by an authenticated empty frame. Complete files also carry size and SHA-256 inside the encrypted message metadata. Truncation, reordering, wrong object keys and tampering are rejected.

A shared group key authenticates membership, not distinct device ownership. Anyone with the group key can construct a peer message. Treat workspace members as trusted collaborators; infrastructure operators without the key cannot read message/file content, but can observe metadata, delete/replay ciphertext, or deny service. Local history/cache are protected by macOS user permissions and disk security, not encrypted by SQLite.

## Tasks

Claim is a conditional SQLite update. A valid task can be owned by one session until its lease expires. Complete validates ownership/lease, saves the result, and queues the response in one transaction. A expired lease can be reclaimed; exactly-once external effects require task-specific idempotency. Session aliases route messages but do not sandbox windows.

## Files

Sender encrypts a selected regular file to a private temporary file and uploads it using OSS V4 authentication. Receiver only accepts object paths under its own workspace/device prefix, downloads/decrypts to a private temp file, checks metadata, then atomically renames. Cache names are confined to a message-ID directory. Concurrent file operations are serialized to bound resource use. Temporary upload files and incomplete downloads are removed; OSS lifecycle handles orphaned remote objects.

## Lifecycle

LaunchAgent starts after macOS user login and can be stopped/uninstalled without destroying state. Connection errors retry with a delay; idle inbound reception uses blocking XREAD. Shutdown cancels requests, closes Redis, waits for workers, and closes SQLite. Configuration changes require stopping/restarting. Local daemon logs contain generic operational errors, not message bodies or credentials.
