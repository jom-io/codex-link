# Contributing

Use Go 1.25+. Keep credentials and local state out of commits. Copy the YAML template only to a gitignored local file.

Before submitting, run `gofmt -w cmd internal`, `go vet ./...`, and `go test -race ./...`. Add behavior-focused tests for delivery, persistence or security changes. Never claim live Redis/OSS verification based only on fixtures. Keep English and Chinese READMEs consistent.

协议或状态库变更需要明确兼容策略；不要静默更换协作密钥、设备 ID 或保留期限。贡献前运行格式化、静态检查和测试，保持中英文文档一致。

Release maintainers push a reviewed `v*` tag; the release workflow tests and uploads Mac archives and checksums. Do not add credentials to workflow files.
