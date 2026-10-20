# rc.8 会话绑定验收

日期：2026-10-20，时间均为北京时间。运行时代码截至 `8867b6b`，尚非稳定发布。
具体消息 ID、原生 ID 和私有队列证据保存在本地忽略目录 `work/session-binding/`，
不把本机路径、聊天上下文或配置副本提交到仓库。

## 已通过

- Go 全量 race 测试、vet；21 项 Node 适配器测试。新增覆盖稳定默认别名、
  数据库重开、禁止 worker/native 接管、双端接受、频道多目标独立任务、
  同机错误来源/错误目标 worker、暂停保留队列、成员撤销后再校验、离组后
  关联结果、MCP 进程重启，以及 native admission 门槛。
- 自动别名使用设备/宿主/首次注册北京时间/短码；office 与指定 home-ds 均
  在不填 name 的情况下导入原完整 worker，原生聊天映射保持一致。
- office 当前真实聊天重复 attach 保持监听器 PID，不生成第二个 receiver；
  两个全新的真实 MCP 进程都恢复同一个端点和 worker，文件工具仍关闭。
- office 暂停后重新 attach 仍为 paused，显式 resume 后恢复。升级后的安装
  脚本已在两台实际 Mac 上成功自动停止/启动 daemon，无需重新 init；新增
  launchd 停止完成与 socket 就绪等待覆盖了首次迁移发现的问题。
- 两台实际 Mac 通过配对加密通道交换逻辑目录、发送精确邀请，并由原 home-ds
  聊天接受。目录没有原生聊天 ID/本机路径；频道保留原完整 worker 路由。
- office 的 4,640 条基线消息，缺失 0、正文/方向改变 0，agent-link 配置摘要
  不变，SQLite integrity_check 为 ok。既有 native 队列与去重日志保留。

- 12:16:30，office 在上一回合结束后由 registry 原生通知启动新回合。通知
  event 与本机 admission、accepted 提交日志匹配；12:16:55 原 worker 认领
  并完成精确探针，匹配 nonce/event 的 result 经 cloud 送达 home。无手动
  “继续”，home → office 的新频道自动模型唤醒通过。私有完整 ID 在本地
  admission/state 记录中，不以旧 nonce-only 结果作为本轮新证据。

- 12:20，home 只对原 home-ds 的插件 entry 执行禁用/启用并更新模块 revision，
  新 registry listener 接替原队列；另一聊天 listener PID 保持不变。原端点、
  默认别名、worker/native 映射与 MCP 文件权限保留。随后 home 的真实模型由
  新 registry 原生通知自动开启，完成原先队列中的精确探针；nonce/event
  匹配，office 的 verified_at 与 verified_event 更新。双方新频道原生模型
  唤醒均通过，不以需要手动输入的历史回合计数。

## 必须区分的结果

初版探针曾在 home 的现有活跃模型回合中被主动领取并回传。这只能证明频道
路由和模型处理，**不计为新频道自动唤醒成功**。`7bd6bb8` 增加了本机 native
admission 记录要求；没有该通知记录时 `session verify` 会拒绝。最终通过须
有匹配 nonce、精确目标 worker、原生通知 event 和后续真实模型回传。普通
native ACK、服务在线或历史成功时间不能代替这个结果。

## 最终独立任务与实机覆盖范围

12:21:45 发出全新定向频道任务，home 在空闲后由新 native event 唤醒，12:21:52
以原 worker 认领，12:22:38 回传正确随机值。office 核对 task/reply_to、频道、
来源设备、来源完整 worker、目标 worker 与随机值全部匹配，route=cloud。
home 已通过 Git ff-only 获取 `5199b75`（运行时代码 `8867b6b`）并安装 rc.8，
Go/Node 检查通过，daemon 正常。此次 Go 升级无需再次加载 Cordis 模块。

home 回报重复 attach 后端点、别名、worker/native 与接收器 PID 均保留；
pause→attach 仍 paused=true，显式 resume 后恢复。另一聊天 listener PID
保持不变。未更改桌面 MCP 配置或文件权限；home 可选的双 MCP stdio 进程
复验未执行，该项已在 office 实机和共享宿主回归中验证。

本轮指定的 office ↔ home-ds 注册、恢复、绑定、暂停和双向自动模型唤醒已
完成验收。以下范围单独保留限制：

- 本轮只联系指定的既有 home-ds。多个真实 DeepSeek 聊天和 home-codex 的
  模型联调尚未作为实机通过；一对多/多对多目前由独立端点回归用例覆盖。
  home-codex 网络限制不因 daemon 在线而解除。

既有 rc.7 双向模型唤醒、local 文件及 LAN 验收见 verification.md；不把那些
结果重复计作本轮新频道/动态注册的验收。
