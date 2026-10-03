# Infrastructure configuration / 基础设施配置

## Redis

Use a password-protected standalone Redis endpoint with TLS and persistence. The application uses Redis Streams, hashes and a Lua publish script. Redis 6.2+ is recommended. Configure AOF/RDB/backups according to your durability requirements and avoid eviction policies that silently remove inboxes.

Restrict the application user to `~cl:v2:*` and the required command families. A starting ACL command allowlist is:

```text
+ping +hello +auth +select +client +xadd +xread +xlen +hset +hgetall +get +set +expire +exists +eval +evalsha +script
```

Adjust it to your managed service and client handshake. `+client` and `+script` are broad categories; narrow subcommands where supported. Do not grant configuration/admin commands. The publish Lua script needs permission for its underlying commands as well as EVAL/EVALSHA. Authenticated ACL users can still alter workspace data if their key pattern permits it; use separate Redis users/prefix policies if stronger isolation is required.

Redis without TLS requires the explicit `allow_insecure` opt-in. Message encryption does not encrypt the Redis authentication exchange. Cluster/sentinel-specific discovery is not implemented.

消息保留默认约 10000 条（含回执），最后写入后 30 天过期。消息被裁剪、过期或基础设施丢失后，本地尚未收取的消息无法恢复。

## Aliyun OSS

Use a private bucket, HTTPS endpoint and prefix-scoped RAM user credentials. V4 signatures use `oss.region`, inferred from standard regional endpoints when omitted. Both devices use the same bucket/prefix. For workspace `personal`, the object root adds a SHA-256-derived workspace segment and destination device ID.

Required operations:

- `oss:PutObject`: sender uploads encrypted objects.
- `oss:GetObject`: receiver downloads attachments.
- `oss:DeleteObject`: connectivity probe cleanup; application transfer otherwise does not automatically delete objects.

Example RAM policy (replace bucket/prefix):

```json
{
  "Version": "1",
  "Statement": [{
    "Effect": "Allow",
    "Action": ["oss:PutObject", "oss:GetObject", "oss:DeleteObject"],
    "Resource": ["acs:oss:*:*:YOUR_BUCKET/codex-link/*"]
  }]
}
```

This grants both devices access under the shared prefix. Use separate policies with device-specific read/write scope if needed. No ListBucket permission is required by the application. Configure OSS lifecycle expiration, e.g. 30 days for `codex-link/`; recovery then depends on the shorter of message and object retention.

STS needs all three values: AccessKey ID, AccessKey secret and security token. This release does not run an STS issuer or automatically refresh credentials. Reconfigure before expiry. S3-compatible endpoints are not supported by this provider.

## Local secrets

`config.local.yaml` is a bootstrap file with plaintext credentials; keep it at 0600, never commit it, and remove/protect it after import. Keychain is the runtime secret store. Ordinary runtime config and database are per-user files. Device private keys are created automatically in Keychain and must not be copied into the YAML. Each new pair is confirmed locally on both devices. Names are discoverable metadata; possession of Redis credentials alone does not establish peer trust. Losing identity keys changes the device ID and requires pairing again.
