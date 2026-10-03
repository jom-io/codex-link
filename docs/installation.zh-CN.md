# 在另一台 Mac 安装并使用 codex-link

本指南针对另一台 Mac 上的 Codex，可按顺序执行。仓库：https://github.com/jom-io/codex-link 。优先使用公开 Release，不需要安装 Go、Python 或 Git。版本变更请查看 [CHANGELOG](https://github.com/jom-io/codex-link/blob/master/CHANGELOG.md)。

## 1. 下载并安装应用与 skill

```sh
curl --fail --location https://raw.githubusercontent.com/jom-io/codex-link/v0.1.3/scripts/install.sh -o /tmp/codex-link-install.sh
CODEX_LINK_VERSION=v0.1.3 sh /tmp/codex-link-install.sh
export PATH="$HOME/.local/bin:$PATH"
codex-link version
```

应用安装到 `~/.local/bin/codex-link`，skill 安装到 `${CODEX_HOME:-~/.codex}/skills/codex-link`。下一轮 Codex 对话可以使用这个 skill。不要依赖终端临时 PATH：后续可调用 `$HOME/.local/bin/codex-link`；需要时将 PATH 设置加入自己的 shell 配置。

安装脚本按当前 Mac 架构选择 arm64 或 amd64 包，并验证 SHA-256。若没有网络或下载受限，可在另一端手动下载对应 Release 压缩包及校验文件后安全传入；源代码 ZIP 不是安装包。不要把 `config.local.yaml` 当成项目源码上传。

## 2. 准备本机配置

```sh
mkdir -p "$HOME/Library/Application Support/codex-link"
curl --fail --location https://raw.githubusercontent.com/jom-io/codex-link/v0.1.3/config.example.yaml -o "$HOME/Library/Application Support/codex-link/setup.local.yaml"
chmod 600 "$HOME/Library/Application Support/codex-link/setup.local.yaml"
```

请让用户在本机编辑 `setup.local.yaml`，不要让用户在聊天里发送密码：

- `name` 使用 `office-mac`（另一台已配置为 `home-mac` 时）；不同机器名称应不同。
- `workspace` 与另一台一致，例如 `personal`。
- 配置同一 Redis 服务的 host:port、用户名、db、TLS 方式和密码。
- 文件传输使用同一阿里云 OSS bucket/prefix，并填写本机拥有的 RAM/STS 凭据。
- 不需要填写任何 `workspace_key`；设备身份与加密密钥由应用生成。

如果 Redis 没有 TLS，用户必须明确设置 `tls: false` 和 `allow_insecure: true`。应用不会自动降低连接保护。仅消息模式可将 OSS endpoint/bucket 置空。

## 3. 初始化、检查并常驻启动

```sh
"$HOME/.local/bin/codex-link" init --config "$HOME/Library/Application Support/codex-link/setup.local.yaml"
"$HOME/.local/bin/codex-link" doctor --files
"$HOME/.local/bin/codex-link" daemon start
"$HOME/.local/bin/codex-link" status
"$HOME/.local/bin/codex-link" peers
```

初始化将云凭据和自动生成的私钥放入 Keychain。普通 `config.json` 不包含密钥。用户登录后，LaunchAgent 会持续启动服务；本机所有 Codex 窗口共享消息历史和文件缓存。导入后请保护或删除含凭据的 `setup.local.yaml`。

`status` 应显示 `redis_connected: true`；启用文件时 `oss_configured: true`。`peers` 应显示本机和另一台设备。初次启动后心跳注册可能需要数秒，先等待再检查。设备同名时使用 ID。

## 4. 首次联系，两台 Mac 分别点击确认

```sh
"$HOME/.local/bin/codex-link" send --to home-mac --session office-helper --conversation first-check --text "office-mac 已完成安装，请回复确认"
```

首次发送返回 `pairing_pending` 是正常状态，两台会出现配对弹窗。用户核对两端六位校验码相同、设备正确，然后在两边点击确认。文字消息自动从发件箱发出，无需复制密钥。只确认一端、拒绝或超时不会放行。

```sh
"$HOME/.local/bin/codex-link" pair list
"$HOME/.local/bin/codex-link" inbox --after 0 --conversation first-check
"$HOME/.local/bin/codex-link" wait --after LAST_SEQ --timeout 30 --conversation first-check
```

`LAST_SEQ` 使用本机已经处理消息的最大 `seq`，不要直接复制另一台的数字。请求十分钟过期；需要时运行 `pair request --to home-mac` 重新发起。无图形环境见 README 中的 `pairing_headless` 和显式确认命令；Codex 不应自行替用户确认陌生设备。

## 5. 文件与任务

```sh
"$HOME/.local/bin/codex-link" file send --to home-mac --path /absolute/path/report.txt --conversation first-check
"$HOME/.local/bin/codex-link" file fetch FILE_MESSAGE_ID
"$HOME/.local/bin/codex-link" send --to home-mac --kind task --session office-helper --conversation setup --text "检查指定软件是否安装并返回版本；不执行安装或卸载"
```

文件接收返回可供本机各窗口访问的绝对路径。首次文件发送触发配对时没有上传文件，确认后需要重试该文件命令。目录由用户明确选择并打包；附件不会自动执行。

执行期间消息仍会由常驻服务接收并存入队列；“delivered”表示本机服务已保存，不代表 Codex 已读取。长时间命令要异步启动，并在轮询命令进程期间定时检查 `task list` 和 `inbox`。收到执行任务后，先运行 `task list --session ALIAS --conversation setup` 找到该任务，复制完整 `message.id`（不要拿 `reply_to` 的父消息 ID）。每个并行工作的 Codex 窗口使用不同且稳定的 `--session` 别名；同名别名会被视为同一租约持有者。先确认用户授权，再 `task claim TASK_MESSAGE_ID --session ALIAS`，立即向发送方回报已认领；执行期间每隔约一分钟或在阶段完成时发送进度。收到独立的新任务时交由另一个空闲窗口使用独立 session 认领；如果没有空闲窗口，先确认已收件并说明排队情况。最后 `task complete TASK_MESSAGE_ID --session ALIAS --text RESULT`。认领报错会指出任务不存在、已经完成或由哪个 session 持有以及租约时间。对端送达回执不等于任务完成。

## 排查

- 找不到命令：使用 `~/.local/bin/codex-link` 绝对路径或修复 PATH。
- `daemon unavailable`：先 `daemon start`；查看用户数据目录 `daemon.log`。
- 没有对端：检查 workspace、Redis 地址/db/ACL 和对端是否注册，不能只检查本机进程。
- 收不到新消息：检查是否两边确认配对；`inbox` 的数字游标必须来自本机。
- OSS 不可用：用 `doctor --files` 检查 endpoint/region、bucket、RAM 权限和 STS 到期。
- 修改配置：先 `daemon stop`，重新 `configure --config ...`，再 `daemon start`。不要删除已有设备私钥。
- 空闲 Codex 没有自动回复：后台应用能收件，但不会自动唤醒空闲聊天；让活跃会话读取收件箱或等待。

仅在检查实际服务和双端结果后报告联调通过，不要将 CI 的模拟服务测试当作两台机器已经通信。
