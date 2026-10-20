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

## 必须区分的结果

初版探针曾在 home 的现有活跃模型回合中被主动领取并回传。这只能证明频道
路由和模型处理，**不计为新频道自动唤醒成功**。`7bd6bb8` 增加了本机 native
admission 记录要求；没有该通知记录时 `session verify` 会拒绝。最终通过须
有匹配 nonce、精确目标 worker、原生通知 event 和后续真实模型回传。普通
native ACK、服务在线或历史成功时间不能代替这个结果。

## 本轮仍待实机完成

- home 从旧的单聊天 DeepSeek 插件首次加载动态 session manager，再从空闲
  聊天接收频道任务。Cordis 的 name-only revision 修改不会加载新模块；本机
  代码与双 entry 离线实验确认：仅禁用/重新启用目标 entry 可以加载新模块，
  unchanged sibling entry 保留。实际加载须核对另一聊天 listener PID 未变。
- office 当前回合运行时，原生通知正确保持 busy/pending；须待本回合结束后
  由原生通知发起新的真实回合，才能完成反向频道唤醒验收。
- 本轮只联系指定的既有 home-ds。多个真实 DeepSeek 聊天和 home-codex 的
  模型联调尚未作为实机通过；一对多/多对多目前由独立端点回归用例覆盖。
  home-codex 网络限制不因 daemon 在线而解除。

既有 rc.7 双向模型唤醒、local 文件及 LAN 验收见 verification.md；不把那些
结果重复计作本轮新频道/动态注册的验收。
