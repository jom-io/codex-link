# agent-link

[English](README.md) · [MIT License](LICENSE)

面向 Codex、Claude Code、DeepSeek Harness 等 AI 编程工具的跨 Mac 加密消息、文件和任务协作组件。各 harness 通过 JSON CLI 或 MCP stdio 使用同一个本地 Go daemon，不接管模型或模型 API 密钥。

当前为 **v0.2 开发版本**：从 codex-link 更名而来，旧命令仍兼容。仓库与 Go module 暂保留 `jom-io/codex-link` 地址；不把开发分支视作已发布的稳定 Release。

## 已有设备从 Git 升级

两台机器拉取同一分支、核对同一提交后构建安装。源码构建要求 Go 1.25+。

```sh
git clone https://github.com/jom-io/codex-link.git agent-link
cd agent-link
git switch codex/agent-link-multi-harness
git rev-parse HEAD
AGENT_LINK_VERSION=v0.2.0-rc.1 sh scripts/install.sh --source "$PWD"
"$HOME/.local/bin/agent-link" version
"$HOME/.local/bin/codex-link" status
"$HOME/.local/bin/agent-link" peers
```

后续在干净工作区 `git fetch origin`，再 `git merge --ff-only origin/codex/agent-link-multi-harness`。不要丢弃本地改动。安装器检测到 `~/.codex/bin/codex-go` 时优先使用它，也支持 `AGENT_LINK_GO` 指定 Go 包装工具；统一复用编译缓存，产物写入固定路径。

升级只短暂停止和重启现有 link daemon。`agent-link` 和兼容命令 `codex-link` 共用设备身份、配对、数据库、文件缓存、Unix socket 和服务。**已有设备不要重新 init，不要改名 Keychain 条目或复制私钥。** 安装后核对设备 ID、配对和历史是否保留。

安装器将旧二进制和已有 Skill 保存在 `$BIN_DIR/.agent-link-backups/<旧二进制SHA256>/`，不删除旧备份。故障时先停止 link daemon，再从报告的备份恢复二进制和 Skill，最后启动。升级失败不会自动回滚，数据与凭据仍保留。不同业务服务不受升级脚本操作。

## 组件边界

- Redis Streams + SQLite 持久发件箱、重试、去重、回执与保留期内的离线补收。
- 独立设备身份、双端人工确认配对、成对加密的消息和 OSS 文件。
- 会话/对话路由、任务领取和续租、事务完成回复、显式 worker 活跃度。
- Go daemon + 私有 Unix socket + Keychain + LaunchAgent，每个 macOS 用户一个服务。
- CLI、通用 Skill、MCP stdio 接入，不依赖特定模型。

消息已送达只表示接收 daemon 已保存，不能证明聊天已读取或任务已执行。收到任务不等于获得执行授权；daemon 不自动执行命令，也不唤醒空闲聊天。会话别名用于路由和租约归属，不提供权限隔离。

## Harness 接入

[接入指南](docs/harnesses.md) 提供 Codex、Claude Code 与其他 harness 的安装及验证方式。CLI 可以从任何允许调用本地工具的 harness 使用；Skill 需放到目标实际支持的目录。DeepSeek Harness 的模型配置和 API 密钥保持不变。

```sh
# 默认只读 MCP 服务，由 harness 作为子进程启动
agent-link mcp --harness deepseek-harness
# 明确允许消息发送和任务领取/进度/完成；文件工具另加 --allow-files
agent-link mcp --harness claude-code --allow-write
```

通过 `agent_link_context` 查询当前 MCP 进程的唯一 session。持有任务租约的重连会话可以显式传 `--session`，不同活跃窗口不能共用 owner。MCP 不提供 shell、配对确认、凭据配置或后台管理工具。stdio 采用逐行 JSON-RPC，工具调用串行，wait 最多 30 秒，无 HTTP 端口。完整 Skill 位于 [skills/agent-link](skills/agent-link/SKILL.md)，旧 `codex-link` Skill 仍可使用。

## 兼容策略

| 项目 | v0.2 行为 |
| --- | --- |
| 新命令 | `agent-link` |
| 旧命令 | `codex-link`，使用同一实现 |
| 数据目录 | 保留 `~/Library/Application Support/codex-link/` |
| Keychain / LaunchAgent | 保留 `io.jom.codex-link` |
| Redis、加密域、OSS 默认前缀 | 保持原协议 v2，可与 v0.1.3 通信 |
| 环境变量 | 优先 `AGENT_LINK_*`，兼容 `CODEX_LINK_*`；显式空的新变量覆盖旧变量 |
| 发布包 | 新旧两个可执行名称及 Skill，兼容旧包名 |

首次配置、配对、Redis/OSS 说明参见 [原安装指南](docs/installation.zh-CN.md)，其中旧命令继续有效。公开网络 Redis 应使用 TLS；配置及密钥仅在本机输入。全新安装使用 `agent-link init`，已有安装只升级。

## 验证与限制

升级检查：双端 CLI/daemon 版本、设备身份、配对、旧消息历史、双向任务/result、必要文件的 SHA256。Harness 集成必须实际调用 CLI 或 MCP 并完成授权范围内的消息交换，不能只凭配置文件存在判定通过。

保留原有能力边界：不保证外部任务副作用 exactly-once，不新增自动执行器、跨用户收件箱、自动 STS 刷新、归档自动解压或其他存储提供方。设备离线恢复受 Redis/OSS 保留策略限制；worker 超时只表示疑似无响应，不能据此重复执行任务。macOS 登录会话和 Keychain 需可用。

开发验证运行 `go test ./...`、`go test -race ./...`、`go vet ./...`；本机有 Go 规则包装工具时均通过该工具调用。打包命令 `sh scripts/package.sh v0.2.0-rc.1`。参见 [架构](docs/architecture.md)、[变更记录](CHANGELOG.md) 和 [验证说明](docs/verification.md)。
