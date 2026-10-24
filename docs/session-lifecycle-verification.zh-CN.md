# rc.11 会话生命周期验收

日期：2026-10-24，时间均为北京时间。运行代码提交 `621dc65`，版本
`v0.2.0-rc.11`，尚非稳定发布。私有原生映射、消息 ID、基线摘要和现场证据保存在
本机忽略目录 `work/lifecycle/` 与 home 对应的本地验收目录，不提交配置或历史正文。

## 自动化覆盖

- 全量 Go race、vet、25 项 Node 适配器测试及 GitHub CI 通过。
- 时钟注入测试覆盖长期 idle/running、等待人类和不确定 admission，不在实机改时钟
  或倒填在线时间。未知状态不清除原有运行保护；旧 receiver 迁移既不伪造在线，
  也不当成从未成功接入的临时登记。
- 覆盖 24 小时临时登记、7 天离线、启动/睡眠重连宽限、pin、预览和实际软归档。
  未完成入站/出站任务、过期但未完成的租约、结果、文件及 journal 保护均有回归。
- 归档后关闭并重开数据库，再从同一原生聊天 attach，恢复原 endpoint、worker、
  alias、native/profile、bindings 和 pause/wake；其他聊天保持。读操作不算处理，
  MCP 写入仍需 opt-in，presence 不作为模型可调用的 MCP 伪造工具。
- 70 条目录完整分页、缺页/乱序/旧快照、缓存失效、旧 wire 兼容及归档桥接器退出
  有回归。DeepSeek 被动观测只使用注入的已加载 agent registry，不调用 resolver
  来加载或恢复历史聊天。

## 两端升级与活跃保护

两台实际 Mac 都在原专用源码目录通过 Git fetch/ff-only 获取相同提交并安装 rc.11。
home 对主提交和最终两文件迁移修复各执行一次有界 HTTPS/HTTP1 拉取，均成功；
备用加密 Git bundle 未采用。未重新 init、安装工具链或改变 VPN、业务与凭据。
仅组件 daemon 和各自当前 Codex 聊天的 sidecar 更新；原 DeepSeek 宿主及其他聊天
不整体重启，MCP 文件权限保留。

office 的 6,958 条基线消息 body/direction、1 个配对、55 个文件映射与配置无变化；
home 的 7,118 条基线消息、2 个配对、74 个文件路径缺失或变化均为零，两端 SQLite
integrity_check 均 ok。home 原 12 条端点核心映射、当前聊天两条绑定、原身份与
别名、pause/wake、其他 binding 文件和既有 DeepSeek listener 保持。

两端被动原生观测均实际报告 online/running、5 分钟租约。限定当前 worker 的
`session gc --dry-run` 与 `--apply` 均返回空候选，归档零条。普通 `session detach`
自行做新鲜原生复核，两端均以 `native-running` 及尚有工作保护拒绝，未通过改时间、
完成他人任务、批量 ACK 或修改 admission 绕过保护。

home rc.11 的新探针在 11:15:57 真实唤醒 office，原 worker 领取自身任务后 verify；
原 home-ds 的探针在 11:18:06 再次开启原生回合。office accepted 10→11→12，
failures=0，事件分别匹配各自精确任务。主动读取不作为模型自动唤醒证明。
新完整目录快照也实际同步成功，当前 home-codex 显示 online，旧登记未知状态仍保守保留。

在原 home-codex 结束预检回合后，11:28:49 的独立随机任务再次由真实原生通知启动模型；
完整 source/target/reply_to、随机值及本 task 的持久化 event/accepted 时间匹配。
home accepted 18→19、failures=0、pending=0，原身份和两条完整绑定保持一致。
这是升级后空闲新任务唤醒证据；由于此前未归档，不能用它代替归档恢复后的唤醒验收。

## 原聊天空闲归档与恢复

11:26 的只读预检发现，原 home-codex 仍有 5 条未完成历史任务、6 条尚无处理回执的
历史文件和 9 条历史结果，涉及其他聊天和业务。预检按相同 lifecycleWork 规则排除
本轮自身任务后仍成立。因此没有启动助手、执行 detach 或归档，也未修改旧任务状态、
下载业务文件或批量确认历史结果。这验证了历史工作会阻止退出，但不能算作实机
“归档→重开恢复→新任务唤醒”通过。该闭环需要无历史工作的独立测试聊天；现有聊天
继续保留身份与工作。数据库关闭重开后的同身份、绑定及权限恢复已有两端通过的
Go 自动化覆盖，须与实机证据区分。

## 范围

软归档只退出组件通信登记并停止其自身接收器，保留历史、身份引用、队列与绑定。
本轮没有关闭原生聊天或清除会话历史；数据库重开恢复由自动化覆盖。已加载的旧
DeepSeek 插件未被强制重启，新被动 presence 在正常插件加载后生效；旧接收器
在状态未确认时保守保护。本轮不宣称真实等待七天或每种 harness 都完成实机归档。
