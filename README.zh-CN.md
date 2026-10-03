# codex-link

[English](README.md) · [MIT 开源协议](LICENSE)

为两台 Mac 上的 Codex 提供加密消息与文件通信。每个 macOS 用户运行一个 Go 常驻服务，本机所有 Codex 聊天窗口通过 CLI 共享收件箱、任务历史和文件缓存。

**当前为首个版本。** 应用负责通信与文件传输；收到任务不会自动执行命令，也不会自动唤醒空闲的 Codex 聊天窗口。文件服务首先支持阿里云 OSS。

## 能力

- 自动生成设备身份，支持自定义名称、首次联系确认配对、协作空间和会话路由。
- Redis Streams 消息传输，SQLite 发件箱、重试、去重和送达回执。
- 离线补收，本机多个聊天窗口共享历史。
- 任务领取、可续期租约、原子保存结果并发送回复。
- OSS 加密文件上传下载、受控内存处理、SHA-256 校验。
- macOS Keychain 凭据及设备私钥存储、双端原生配对确认、Unix socket 本地访问和 LaunchAgent 常驻启动。
- Codex skill、GitHub Release/源码安装、升级、回退及卸载。

## 架构

```text
Mac A                                 Mac B
多个 Codex 窗口                        多个 Codex 窗口
     │                                     │
skill → CLI                           skill → CLI
     │                                     │
Unix socket → Go 常驻服务              Unix socket → Go 常驻服务
     │ SQLite + Keychain                   │ SQLite + Keychain
     └──────────── Redis Streams ──────────┘
     └──────────── 私有阿里云 OSS ──────────┘

GitHub 仓库 / Releases：源码、skill、Mac 安装包
```

两端不需要开放公网入站端口。Redis 保存签名的设备发现/配对事件和加密的业务消息，OSS 保存加密文件。基础设施仍可见设备名、公钥、路由、对象路径、时间和大小。每台设备自动生成 Ed25519/X25519 身份；首次联系时两端确认，应用自动派生每对设备独立的加密密钥，无需填写或复制密钥。会话别名不是窗口间权限边界。

## 安装

### Release 安装（推荐）

下载并检查指定版本的安装脚本后运行：

```sh
curl -fL https://raw.githubusercontent.com/jom-io/codex-link/v0.1.0/scripts/install.sh -o /tmp/codex-link-install.sh
CODEX_LINK_VERSION=v0.1.0 sh /tmp/codex-link-install.sh
export PATH="$HOME/.local/bin:$PATH"
codex-link version
```

脚本自动选择 Apple Silicon 或 Intel 包，验证 Release 的 SHA-256，将应用安装到 `~/.local/bin`，同时安装 skill。校验文件用于发现下载损坏，不是独立的发布者签名。当前二进制没有 Apple 签名或公证。对应 Release 发布后，上述下载链接才可用。

### 源码或源码 ZIP 安装

源码编译需要 Go 1.25+；Release 安装不需要 Go 或 Python 环境。

```sh
git clone https://github.com/jom-io/codex-link.git
cd codex-link
git checkout v0.1.0
sh scripts/install.sh --source "$PWD"
```

使用源码 ZIP 时，解压后执行同样的源码安装命令。尚未发布时，可直接编译已检出的 `master` 源码。

## 两台 Mac 分别初始化

复制模板，在本机编辑，不要将真实凭据发到聊天里：

```sh
cp config.example.yaml config.local.yaml
chmod 600 config.local.yaml
codex-link init --config "$PWD/config.local.yaml"
codex-link doctor --files
codex-link daemon start
codex-link status
codex-link peers
```

`config.local.yaml` 已加入 Git 忽略规则。两台机器的 `workspace`、OSS bucket/prefix 相同，设备名不同。设备身份自动生成，私钥保存在 Keychain；不再需要配置 `workspace_key`。

| 配置项 | 含义 |
| --- | --- |
| `name`、`workspace` | 1–64 个 ASCII 字母、数字及 `. _ -` |
| `redis.address` | `host:port`，不带 URL 协议前缀 |
| `redis.username`、`redis.db` | Redis ACL 用户名和数据库编号 |
| `redis.tls` | 公网 Redis 使用 TLS |
| `redis.allow_insecure` | 无 TLS 服务需显式开启；此时 Redis 密码会以明文传输 |
| `oss.endpoint`、`oss.region` | 阿里云 HTTPS 区域 endpoint；标准域名可自动推导 region |
| `oss.bucket`、`oss.prefix` | 私有 bucket 和共用对象路径前缀 |
| `credentials.redis_password` | Redis 密码 |
| `credentials.access_key_id`、`access_key_secret` | 限定 prefix 权限的 RAM 凭据 |
| `credentials.security_token` | 可选 STS token；到期前需手动重新配置 |
| `max_file_bytes` | 默认单文件最大 1 GiB |
| `stream_max_len` | 每设备消息流约 10000 条，包含回执，超出会裁剪 |
| `stream_ttl_hours` | 最后写入后过期时间，默认 720 小时 |

只使用消息时，将 OSS endpoint 和 bucket 改为空字符串。Redis ACL 和 OSS 权限说明见[基础设施配置](docs/infrastructure.md)。`doctor --files` 执行小文件加密上传、下载和删除探测，不代表生产持久性验证。

初始化将凭据导入 Keychain（service 为 `io.jom.codex-link`，account 为设备 ID）；应用保存的 `config.json` 不包含密码或密钥。导入后请保护或删除填写后的 YAML。交互式 `init` 隐藏密码输入，也支持 `--secrets-env` 从环境变量读取凭据，变量名称见英文 README。

## 首次联系：两边点击确认即可

直接发送，例如 `codex-link send --to office-mac --text "你好"`。尚未配对时，消息先保留在发件箱，两台 Mac 分别弹出确认窗口。核对两端显示的六位校验码相同、设备名称和 ID 正确，再点击确认。应用固定对端身份并自动生成加密通道，之前排队的消息随后自动发送。

只确认一端或选择拒绝，不会发送业务消息。增加第三台设备时，分别确认新的设备配对，不能直接访问已有设备间的通道。请求十分钟过期，弹窗两分钟超时；拒绝或过期后需要主动重新发起。重装丢失身份密钥会产生新设备 ID，需重新配对。

无图形会话或需要显式命令时，可设置 `pairing_headless: true`，在用户确认后执行：

```sh
codex-link pair request --to office-mac
codex-link pair list
codex-link pair accept REQUEST_ID --code 123456
# 拒绝：codex-link pair reject REQUEST_ID --code 123456
```

普通 Mac 使用无需手动执行配对命令。首次发送文件会先发起配对并返回 `pairing_pending`，确认后重试文件命令，skill 会负责该步骤；排队的文字/任务会自动发送。

## 消息与任务协作

```sh
codex-link send --to office-mac --text "请检查开发环境"
codex-link send --to office-mac --kind task --session requester --conversation setup --text "检查 Go 版本并返回结果，不要安装软件"
codex-link inbox --after 0 --session worker --conversation setup
codex-link task claim TASK_ID --session worker --lease 900
codex-link task complete TASK_ID --session worker --text "Go 版本已验证：……"
codex-link wait --after 12 --session requester --conversation setup --timeout 30
codex-link history --after 0
codex-link get MESSAGE_ID
```

通信命令输出 JSON；ID 放在参数之前。`--after` 是本机 SQLite 的数字游标，处理后推进到已处理记录的最大 `seq`，不能用另一台机器的游标。读取不会删除消息。`inbox/wait` 读取收到的消息，`history` 包含收发双方，内部回执不展示。

`wait` 最长 60 秒。默认 `--after 0` 可以返回已有消息；`--after -1` 从当前末尾开始，仅在明确不需要旧消息时使用。结果达到条数上限时，先继续分页，再等待。

发送时 `--session` 表示发送窗口别名，`--to-session` 指定对端窗口别名；接收时 `--session` 包含对应窗口消息和公共消息。窗口别名由调用者提供，不依赖 Codex 内部聊天 ID。筛选只用于路由，不是窗口间的权限边界。

`pending` 为本地排队，`sent` 为 Redis 已接收，`delivered` 为对端已保存，均不等于任务已完成。任务内容不自动赋予执行权限，Codex 在用户授权范围内执行。租约默认 15 分钟，同一会话再次领取可续期；超时可被其他窗口接手，因此安装等操作应先检查实际状态，避免重复执行。

## 文件传输

```sh
codex-link file send --to office-mac --path /absolute/path/report.zip --conversation setup
codex-link file fetch FILE_MESSAGE_ID
```

接收后返回验证通过的本地绝对路径，本机各窗口均可访问。目录需要用户明确选择并打包，程序不自动解压或执行附件。

上传先在本地临时文件中分块加密，再流式传到 OSS；下载到临时文件，完成认证、大小和 SHA-256 校验后才发布缓存路径。内存受控，但需要约等于待上传/下载文件大小的临时磁盘空间。上传后发送失败可能留下孤立对象，应配置 OSS 生命周期清理。

## Codex skill

安装脚本将 [skill](skills/codex-link/SKILL.md) 安装到 `${CODEX_HOME:-~/.codex}/skills/codex-link`，之后的 Codex 对话轮次可使用。例如：

> 用 codex-link 让 office-mac 检查 Go 是否安装，并把验证结果返回。

首次配对弹窗不依赖聊天窗口活跃状态。后台服务持续接收，活跃窗口通过等待或读取收件箱获得消息；空闲窗口不会自动启动任务。skill 包含安装配置引导、命令说明、领取任务、发送进度和完成回复流程。

## 运维、升级、回退和卸载

```sh
codex-link daemon stop
codex-link configure --config /absolute/path/config.local.yaml
codex-link daemon start
# 升级：使用新的 CODEX_LINK_VERSION 重新执行安装脚本
# 卸载：sh scripts/uninstall.sh
```

默认数据目录为 `~/Library/Application Support/codex-link/`，包含普通配置、SQLite、缓存、socket 和日志。`CODEX_LINK_HOME` 可覆盖，路径需满足 Unix socket 长度限制。LaunchAgent 位于 `~/Library/LaunchAgents/io.jom.codex-link.plist`，每个 macOS 用户只支持一个受管理服务。用户登录后启动，不能在登录前运行；Keychain 需可访问。

升级保留数据和上一版本应用/skill。回退时停止服务，恢复 `.previous` 应用及对应 skill，再启动。卸载保留历史、缓存及 Keychain 凭据。当前本地历史、缓存和日志需在停止服务后手动清理；不要在运行时删除 SQLite 文件。

## 可靠性与当前边界

- SQLite 发件箱支持重启恢复，Redis 原子写入在保留窗口内按消息 ID 去重。
- 接收消息、保存游标和生成回执在一个 SQLite 事务中完成。
- 传输支持至少一次投递及去重，不承诺业务操作恰好执行一次。
- 离线补收受消息条数裁剪、过期、Redis 持久化和 OSS 生命周期限制，需自行配置 Redis 持久化及备份。
- 设备发现记录带签名，已确认的设备名称和公钥保存在本机以支持离线查询；心跳每 30 秒一次，超过 90 秒未更新可视为离线。重名时使用设备 ID。
- 尚未实现 STS 自动续期、空闲 Codex 自动唤醒、图形界面、自动目录解压、跨 macOS 用户共享或 S3 服务。
- 项目不包含云服务凭据；真实公网和双 Mac 联调需配置实际服务。

## 开发与验证

```sh
go test ./...
go test -race ./...   # race 运行时需要可用的 C 编译工具链
go vet ./...
go test ./internal/link -run '^$' -bench BenchmarkFileEncryption -benchmem
sh scripts/package.sh v0.1.0
```

测试覆盖自动密钥协商、双端确认/拒绝、新设备隔离、消息加密认证、文件截断/篡改/限额、SQLite 持久化、并发领取、送达回执、两个后台实例协作和离线补收。Redis 使用内嵌测试服务；OSS 使用 TLS HTTP 模拟服务和 SDK 签名请求，不替代真实 OSS 权限验证。生产程序使用纯 Go SQLite，支持 `CGO_ENABLED=0` 编译。

GitHub Actions 对推送和 PR 执行测试；推送 `v*` 标签后测试并发布两种 Mac 包和校验清单。详见[架构说明](docs/architecture.md)、[验证记录](docs/verification.md)和[贡献指南](CONTRIBUTING.md)。

## 开源协议

MIT，版权归属 2026 jom-io。依赖保留各自协议，见[第三方声明](THIRD_PARTY_NOTICES.md)。
