# 聊天注册、绑定与重开恢复（rc.8）

一个真实聊天对应一个持久 worker。别名只用于选择和显示；重开、改名和 MCP
进程重启不改变端点 ID 或租约 owner。默认别名形如
`office-codex-1020-1148-abcd`、`home-ds-1020-1149-12ef`：设备、宿主、首次注册的
北京时间和端点短码，生成后固定。用户提供 `--name` 时使用指定名称；同名时
列出候选，必须用完整端点 ID 选择，不猜最近活跃的窗口。

## 每个聊天首次使用一次

在当前聊天的 shell 中：

```sh
agent-link session attach --wake
agent-link session current
```

Codex 使用当前进程的 `CODEX_THREAD_ID`，DeepSeek 使用 `DSH_SESSION_ID`。
其他宿主应显式提供自己的 `--native-session`、`--harness` 和 `--profile`；
缺失上下文时报错，绝不扫描其他聊天代替。默认 profile 是 `default`，不同
宿主配置实例必须保持自己的 profile 值一致。注册命令可选 `--name`。

已有 Codex 原生绑定自动导入原 worker、过滤范围和状态队列。DeepSeek 升级后
首次加载动态插件时导入配置中的原 worker/native ID，不创建替代身份。它的
新插件只需要加载一次，后续同一 profile 的聊天注册动态生效，无需再为每个
聊天编辑 Cordis patch。首次从旧插件切到新插件仍需宿主加载新模块；复制
文件不等于已加载。已验收 DSH 的 Cordis loader 仅改 name revision 不会加载
运行中的新模块；首次迁移需仅禁用目标 entry 并应用，再启用该 entry、修改
revision 并应用。未变化的 sibling entry 保留；操作前后仍核对其 listener。
新动态管理器加载后，新聊天注册不再重复这两步。

Codex 的 `--wake` 自动核对本机原生 owner，并安装这个聊天自己的 sidecar；
重复 attach 复用已有运行监听器。保留旧队列、原生提交日志和去重记录。原生
API 不可用时报告“已注册，接收器未启用”，不退回其他聊天或改审批设置。
注册/修改绑定本身无需重启 daemon 或桌面应用。只注册而不启用接收器可省略
`--wake`；Claude Code 等尚无原生适配器的宿主仍可手动 CLI/MCP 收件。

共享 MCP 进程中用 `agent_link_session_attach`，传入**这个聊天实际的宿主
上下文** `native_session` / `harness` / `profile`，后续每个工具显式传返回的
完整 `session`。MCP 重启后再次 attach 同一上下文，恢复原 worker。共享
进程默认 session 无法识别是哪个聊天；工具参数也不是受信的宿主身份凭据。
无法取得本聊天上下文时，应先在这个聊天的 shell 中注册，不能猜测别的 ID。
旧 `worker new` 与 MCP `--session` 仍兼容，但没有自动恢复能力。

## 选择具体聊天并建立双向范围

```sh
agent-link session list --to home-mac  # 异步请求已配对设备的目录
agent-link session list               # 送达后读取已缓存的端点
agent-link bind TARGET_ENDPOINT_ID --channel starx-dev
agent-link bindings
```

目录只公布已注册的逻辑端点；不传原生聊天 ID、本机路径、标题、聊天正文或
凭据。`directory_requested` 仅表示已排队；对端没有目录回包时，可能旧版本或
离线，继续用旧设备/worker 寻址。普通送达回执不证明支持新功能。

接收方在被选择的**同一个聊天**中查看邀请并接受：

```sh
agent-link bindings
agent-link bind --accept INVITATION_MESSAGE_ID
```

邀请只保存元数据，不唤醒模型，也不能替接收端授权。需要它立即处理邀请时，
通过已经授权且已经启用的旧绑定发一条定向任务；新聊天首次仍需本地加入。
接收方可在接受时加 `--text-wake`，允许这个来源发来的定向文本唤醒；默认
只对 task/result 唤醒。来源设备、来源完整 worker、频道和接收端点全部匹配。
设备配对不能代替聊天接受，组成员也不传递设备信任。

接受后自动排队非秘密探针。目标模型在真实聊天中认领探针自己的任务 ID，
执行 `agent-link session verify --probe ID --session RETAINED_WORKER`，返回匹配
随机值后，发起方 bindings 的 `verified_at` 才更新。原生适配器还必须先为这条
通知保存本机 admission 记录；当前活跃回合主动轮询收到探针不能完成验证。
`verified_event` 关联具体原生通知，记录接口不对 MCP 模型工具开放。`accepted` 只是绑定完成，
接收器 connected 是本机原生连接观察，均不等于模型完成。版本变化会在接收器
重新接入时排队一次探针；普通重开不重复。新失败应结合 wake status 与原生
日志诊断；不要把旧的成功时间或 daemon 在线当作模型此刻可用。

## 日常通信与多对多

```sh
agent-link send --channel starx-dev --to TARGET_ENDPOINT_ID --kind task --text '已授权任务'
agent-link send --channel starx-dev --all --kind task --text '明确群发的已授权任务'
agent-link inbox --channel starx-dev
```

CLI 从当前原生上下文恢复 session；shell 不保留宿主环境时显式传完整
`--session`。只有一个已接受的对端时可省略 `--to`；多个对端必须点名或明确
`--all`。群发使用当时的已接受成员快照，分别产生 message/task ID、租约和
结果，返回每个接收者的排队结果，不把局部成功当作全部执行完成。

`bind A B --channel NAME` 可邀请多个端点。组内其他端点可以继续明确邀请
同组端点，但每条新关系仍需接收方接受；没有自动扩展信任或组内转发。不同
组各自保存范围；名称冲突时使用完整 channel ID。底层沿用 conversation
字段保存稳定频道 ID，`--channel` 与旧 `--conversation` 不能混用。
文件可用 `file send --channel ... --to ... --path ...`，CLI/MCP 仍保留原文件
权限。回复默认返回原请求 worker，不广播。

```sh
agent-link session pause
agent-link session resume
agent-link unbind --channel starx-dev
```

暂停不推进收件游标、不丢待唤醒队列，重开也保留暂停。离组停止这个组的新
唤醒，原始消息/历史/租约保留；已开始任务的精确关联 result 仍可返回原
请求端点，是否唤醒受本机暂停控制。不会给另一个同名聊天接班。

同用户仍共享 daemon、历史及文件缓存；这里是路由和唤醒范围，不是安全沙箱。
宿主忙碌、审批、断网、退出或不确定的原生提交继续沿用既有等待/隔离行为，
不会因为窗口看似安静而抢占任务。
