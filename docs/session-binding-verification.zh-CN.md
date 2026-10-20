# rc.8 / rc.9 会话绑定验收

日期：2026-10-20，时间均为北京时间。rc.8 运行时代码为 `8867b6b`，rc.9 同机
结果校验修复为 `83bf658`，尚非稳定发布。
具体消息 ID、原生 ID 和私有队列证据保存在本地忽略目录 `work/session-binding/`
与 `work/home-codex-onboard/`，
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

上述首轮指定的 office ↔ home-ds 注册、恢复、绑定、暂停和双向自动模型唤醒
已完成验收。多个真实 DeepSeek 聊天的模型联调未执行；另一聊天的 listener
保持原样，不把共享 daemon 在线当作另一个模型可用。

## 后续真实 home-codex 接入

13:52，用户在指定的 home-codex 聊天发送一次接入启动指令。该聊天从自身
宿主上下文导入原完整 worker，13:53 注册生成稳定默认别名，并安装本聊天
专属原生接收器。没有选择历史活跃窗口、重建 worker 或改变 home-ds 的绑定。
本机 CLI/daemon 均为 rc.8，安装的适配器与 `8867b6b` 后的运行时代码匹配。

office 独立核对配对设备逻辑目录中的 Codex 端点/worker/harness，再将其邀请
加入已有组；home-codex 13:55 在同一个真实聊天核对并接受精确邀请。双方在
原准备回合中都将探针留在队列，结束回合后才由原生收件通知启动新模型回合。

13:57，home-codex 以原 worker 认领并完成精确探针，本机 admission、随机值、
通知 event 与返回结果匹配，office 的绑定验证状态更新。同一分钟 office
也由真实原生通知启动新回合，完成反向探针；home-codex 核对其绑定验证事件。
13:58，home-codex 自动完成独立定向任务，返回正确随机值，并提供该任务与
探针同批原生通知的关联证据。两方向均为 cloud 路由；这证明两个实际 Codex
聊天的自动模型唤醒，不把 LAN/local 或普通送达作为本步骤结论。

home-codex 首次接入无需重启共享 daemon、DSH 或 Codex 桌面应用。原设备身份、
DSH patch、agent-link 配置及 MCP 文件权限保持。首次 Git 文档更新因 HTTP/2
网络错误失败，当时源码为干净的 `5199b75`，运行时已含 `8867b6b`。随后仅对
一次 Git 请求使用 HTTP/1.1，成功 fetch 并 ff-only 同步到 `d44a5ea`，没有更改
全局 Git/网络设置；不能将第一次失败本身表述为已同步最新源码。

## 同机原生联调与 rc.9 回归

14:01，由 home-codex 向指定的原 home-ds 发出精确邀请并由该聊天接受，沿用
同一个组 ID；未接入另一 DeepSeek 聊天。两端结束准备回合后，由各自原生
通知启动实际模型，完成双向探针和两个独立随机值任务，所有本机任务及结果
均为 `route=local`。这次接收者包括真实 Codex 和真实 DeepSeek 模型，区别于
早期 CLI 测试发布者与单个 DeepSeek 模型的同机验收。

本次实机发现 rc.8 的绑定验证时间仍为空：本机投递将发送任务的单条共享行
改为 `direction=in`，但发送端结果关联只接受 `out`，因此静默忽略了合法的
native proof。同一个判断也影响离组后仍在途的同机任务结果唤醒。

`83bf658` 只对本设备自发、自收且已标记 `route=local` 的共享行保留发送端
关联语义，继续核对完整来源/目标 worker、频道、任务关联和随机值，不接受
任意外部收件行。新增单 daemon 下 Codex/DeepSeek 两端点回归，在修复前同时
复现上述两处失败，修复后通过；还验证无 native admission 时仍拒绝探针，
以及无关本机 worker 不能冒用离组后的结果。Go 全量 race、vet、21 项 Node
检查和该提交 GitHub CI 均通过。

两台 Mac 均通过 Git 获取该修复并安装 rc.9；home 使用原有运行环境与源码
目录，只重启共享 agent-link daemon。home 的 5,510 条消息、2 个配对、63 个
文件映射缺失为 0，正文/方向/路径变化为 0，SQLite 检查正常；配置、DSH patch、
原生适配器、两个指定聊天监听 PID 与映射保持。office 的 4,640 条基线消息 ID
缺失为 0，agent-link 配置摘要不变，SQLite 检查正常，重复 attach 保持原监听。
此次适配器代码未变，无需重启模型宿主或重新授权文件工具。

14:14 与 14:18，home 两个真实模型在准备回合结束后的新原生通知中分别完成
全新 rc.9 探针。两个本地发送端的 `verified_at`、`verified_event` 与
`verified_version=rc.9` 均真实更新，随机值及 task/result 对应关系匹配，路由
均为 local。保持连接的 DeepSeek 监听器在 daemon 升级后通过原会话接口重新
报告实际 receiver connected，排队反向新版本探针；未伪造 admitted，也未
手工写验证字段或重复使用旧成功记录。

截至 14:20，两个全新独立随机值任务均由目标模型以原 worker 认领、完成并
返回正确随机值，任务与结果四条记录全部为 local。office 独立核对两个实际
聊天的回报、完整 sender/target worker、频道、关联 ID、原生事件和版本；
office 两条已接受关系的验证状态也均更新为 rc.9。用户除首次接入启动消息
外无需再次输入“继续”，后续准备、升级与验收均由原生收件通知推进。

本轮完成三个指定真实聊天的各条定向关系及两模型同机往返，不等于所有
多窗口/多对多/部分离线场景的实机验收；这些更广组合仍由回归用例覆盖。
用稳定频道 ID 发邀请时，新关系的 `channel_name` 可能显示为该 ID；实际
频道 ID、目标聊天别名与接收范围正确。本次保留该显示差异，没有为改名
创建新组或扩展接收权限。文件本轮未重新传输，文件权限保持原状。

既有 rc.7 双向模型唤醒、local 文件及 LAN 验收见 verification.md；不把那些
结果重复计作本轮新频道/动态注册的验收。
