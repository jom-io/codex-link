# rc.10 已处理回执与原生唤醒验收

日期：2026-10-20，时间均为北京时间。运行时提交 `81faaa0`，尚非稳定发布。
完整消息 ID、随机值、原生 event 与私有状态证据保存在本机忽略目录
`work/consumed-wake/`，不提交原生聊天标识、配置、历史正文或本机副本。

## 修复范围

原生聊天忙碌时，模型可能主动读取并处理到结果；旧版没有模型处理完成记录，
回执仍在 deferred 队列中，结束回合后会再次通知。rc.10 增加当前接收 worker 的
精确 `inbox ack` 与现有 write opt-in 下的 MCP `agent_link_inbox_ack`。读取保持只读，
确认仅写本机 metadata；扫描和提交前重检均过滤已确认消息，重启后仍有效。
不删除历史、不推进游标、不发远端确认消息。未完成任务、其他聊天及出站记录
会被拒绝，批次原子且重复确认安全。任务仍须原有 claim/complete 流程。

## 自动化与实际接入检查

- Go 全量 race 与 vet 通过，21 项 Node 适配器测试通过，GitHub CI 通过。
  新回归在修复前失败，修复后通过，覆盖忙碌排队、数据库及监听器重开、
  批次回滚、当前会话范围、保留原记录及新任务/result 仍可唤醒。
- 两个真实 stdio MCP 进程确认只读模式没有 ack 工具，显式 write 模式可对本
  聊天已处理 ID 重复确认并返回 0；文件工具始终关闭。
- office 原有两条忙碌排队结果经显式确认后从队列移除，accepted 保持 8，
  failures=0。之后真正结束模型回合，15:35 的新 native event 只包含两条新的
  rc.10 探针，accepted 8→9；原 worker 在原生新回合 claim+verify 两条探针。
  已处理的旧结果未混入通知，也没有手工写 admission 或 verified。
- home 的非秘密随机样本先真实进入 busy/pending；处理后 ack=1，重复=0，
  历史整行未变，pending 移除，accepted 10→10。合法升级探针保留到原生新回合
  才执行 verify，不把主动轮询当作自动模型唤醒证据。

## 升级与保留

两台实际 Mac 均按提交安装 rc.10，不重新 init。office 直接由 Git checkout 构建；
home 的单次 HTTPS/HTTP1 fetch 超时后，使用配对加密传送的 10,972 字节增量 Git
bundle，校验 SHA256、前置提交、advertised HEAD、Git 对象及 diff 后 fetch/ff-only。
没有复制覆盖源码、改全局 Git/网络配置或安装新工具链。

office 的 5,630 条基线消息 body/direction、1 个配对、36 个文件映射及配置无变化；
home 的 5,814 条基线消息、2 个配对、65 个文件映射无缺失或变化，两端 SQLite
integrity_check 均 ok。原 worker/native 映射、默认别名和 MCP 文件权限保留。
仅两端当前 Codex 聊天的 sidecar 因适配器内容变化重载；原 home-ds 与另一聊天
listener 保持，未重启整个桌面宿主或更改业务、VPN、凭据。

## 边界

显式确认必须在模型实际处理后执行，普通查看不取消必要唤醒。确认只能取消尚未
被宿主接纳的通知；已进入聊天的原生提示无法撤回。最后一次重检到原生提交间
仍有短暂竞态，不承诺业务效果 exactly-once。此修复不增加服务、传输或权限开关。

## home 空闲窗口与新任务

home 完成准备后 accepted=12、pending=[]、failures=0。office 收到并确认阶段结果后
额外等待超过 35 秒，再发独立随机值任务。新任务在 15:50:42 由真实原生事件开启
home 的新模型回合，accepted 仅 12→13；事件只关联该新任务，随机值及精确
source/target/reply_to 均匹配。与控制阶段完成 result 相隔 129 秒。旧样本及
已处理 result/file 均没有 native-admission，重复 ack=0、历史整行未变。
这同时证明空闲期间未因旧回执重复通知，以及新工作仍能自动唤醒模型。

## office 反向空闲窗口与新任务

office 在活跃回合实际处理三条阶段结果，观察到结果位于 busy/pending 后精确确认。
队列清空，accepted=9、failures=0，重复确认返回 0，保存原消息整行供比对。
15:54:36 保存静默基线后结束模型回合。home 在前台等待至少 40 秒才发出全新的
独立任务；15:57:24 office 由真实 native 通知自动启动新回合，距静默基线约
168 秒。accepted 仅 9→10，事件仅关联这条新任务；原 worker/native 映射匹配，
三个旧 result 仍无 native-admission，原历史记录完全相同。原 worker 领取新任务
自身 ID 并完成，返还正确随机值及真实 event。未使用手工“继续”、CLI resume
替身或伪造 admission/verified 记录。

本轮两端均验证“活跃时处理并确认的结果不会在空闲后再次通知”以及“新任务仍能
原生唤醒真实模型”。这不把 native socket ACK、daemon 在线或主动轮询当作模型
自动唤醒成功。反向结果交回 home，仅以本机精确确认和一次 progress 结束协作，
不启动 acknowledgement-only 回信循环。
