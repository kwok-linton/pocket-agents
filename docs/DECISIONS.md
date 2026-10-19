# 决策记录

- D01：产品暂名 Pocket Agents，独立 Go + React/TS/Vite 实现，生产 PWA 嵌入单一二进制。
- D02：Codex 优先连接当前安装版本的原生共享 daemon（Unix socket）；Claude 优先验证原生后台控制，若无公开稳定协议则使用隔离的持久 PTY。不得静默启动竞争 writer。最终能力由实测决定。
- D03：provider profile 显式标识数据目录与 CLI 环境；默认当前用户。Hub 不保存 provider 登录凭据。
- D04：应用动作在 Hub/Node 持久去重，区分 submitted/accepted/unknown/rejected。网络写入不等于 provider 接受。

- D05（M0实测）：Claude 2.1.302官方后台会话及attach支持同一原生进程的多个终端入口，选用该路径，无tmux运行依赖。已有普通无控制通道交互进程只读。默认Claude配置目录保持原环境unset语义，固定绝对CLI路径避免多版本误选。
- D06（M0实测）：Codex共享原生daemon使用Unix WebSocket；本机daemon0.163.0、CLI0.162.1均记录。执行代次来自daemon socket与原生writer锁生命周期；连接epoch独立绑定审批，Node重连不冒充新执行实例。其他desktop/private writer不自动resume。
- D07：Codex当前thread/unsubscribe实测不终止native执行，产品不伪装终止；该provider只提供真实interrupt和原生终端管理入口，明确列入剩余功能限制。
- D08：Claude权限/提问使用真实原生终端fallback，不伪造结构化request ID。终端回答token绑定当前execution+native tool use+wait state，已提交的等待回答token失效；普通聊天用native transcript确认接受。
- D09：Web Push由Hub读取持久native事件发送，VAPID仅本地保存，默认内容仅状态与会话深链接。前台WebSocket只是刷新通道；不宣称锁屏Socket可通知。
- D10：每个应用data目录只允许一个后台服务进程；动作双层持久去重，终端只读轮询不写动作日志。provider失联保留历史但运行状态变unknown并禁止控制。
- D11：Claude首次目录信任用短期原生PTY启动对话承载，在原生分配session ID前只保存startup动作，不将临时ID伪装成Session。原生ANSI输出先按终端文本解析ACK；关闭Node只结束未完成launcher，不停止已经创建的原生后台实例。
- D12：M2 早期在等待第二台设备跨 tailnet 迁移授权时，使用已许可 SSH 的 loopback 加密转发验证独立多设备逻辑；该证据不等同正式 Tailscale 或真手机。后续用户授权登录已完成，正式 Node 已迁移到 Tailscale HTTPS/WSS、撤销临时转发，最终能力以分别记录的正式路径实证为准。
- D13：扫码登录使用本地生成的短时一次码，owner认证才能创建、五分钟有效、单次原子消费。永久owner token仅留Hub本机；URL fragment消费前清除，不进入HTTP查询/普通日志/本地草稿。用此满足手机首次接入而不要求手输长密钥。
- D14（真机推送故障）：浏览器持有 PushSubscription 不再直接等于“已启用”。Hub 返回 `activeSubscriptionIds`（endpoint SHA-256），前端与本机订阅摘要核对；失效时由用户重新启用并重建订阅。同 endpoint 登记更新 keys、保留 cursor。HTTP 410 删除失效订阅，HTTP 201 仅表示上游接收，依据 [Web Push Protocol](https://web.dev/articles/push-notifications-web-push-protocol)；不把此校准机制当作手机送达回执。
- D15（发送参数）：本地核查锁定 `webpush-go v1.4.0` 会为非 HTTPS Subscriber 添加 `mailto:`，传库前规范化已有前缀以避免重复。需及时显示的原生完成/待处理事件设置 `UrgencyHigh`；同 tag 后续通知设置 `renotify=true`，参照 [Notification behavior](https://web.dev/articles/push-notifications-notification-behaviour)。这些是已验证代码问题修复及行为选择，不据此宣称它们是某次真机未收到通知或 410 的根因。
- D16（诊断边界）：Service Worker 仅本地保留最近 20 条 `{eventId, stage, at}`，区分 push 收到、显示 API resolved/rejected 与点击；不记录正文、endpoint、密钥或原始错误。`showNotification()` 成功只能说明 API 成功，不能标“锁屏已显示”。本机测试通知不经过 FCM，不替代真实原生完成事件与物理手机验收。
- D17（通知点击恢复）：通知点击必须使用导航返回的 WindowClient，并在已有客户端无法导航或唤起时回退 `openWindow()`；不能因选择到旧的未受控客户端而直接丢失点击。该缺陷经代码审查确认，但不能断言它是所有真机点击无反应的唯一原因。
- D18（工作范围）：通过共享关注项目设置筛选概览、设备树、会话与通知；这属于显示偏好，不扩大节点目录权限。原生历史保留，可显式查看范围外记录。
- D19（活跃与工作分开）：主会话原生实例存活即可计活跃，只有本轮原生 working 证据才计正在工作；需要处理要求存活实例的真实等待/pending，完成未读与历史错误不自动变成待办。退出 Claude 的旧 waiting/working 降为 unknown 并清空 pending；外部独占 Codex writer 可显示存活，但无可验证 execution ID、控制禁用。不按估计修改原生证据，也不把 unknown 说成确定 idle。
- D20（未知实例与选择一致性）：execution unknown 单独显示“运行状态待确认”，不视为已退出历史。项目标签可直接筛选，活跃与历史使用同一选择。偏好 GET/PUT、目录浏览响应只允许当前请求更新界面，防止反序响应覆盖后来的范围或目录选择，并有对应回归测试。

## 2026-10-17：现有 Claude 交互进程的原生 inbox 与附件通道

此前将所有 interactive Claude 判为只读遗漏了已安装版本的原生 inbox。官方 cross-session messaging 文档确认脚本可以通过 socket 投递；帧格式参考公开协议说明，独立实现，没有复制其他项目的代码。实机 2.1.302 的 registry 包含 peerProtocol=1、messagingSocketPath 和 UTC procStart。实现按 profile、session ID、PID/start time、当前用户拥有的私有 socket 验证目标；不会启动新 writer、终止原实例或改变 inbound/permission 设置。消息仍具有原生 peer 语义，不能充当审批，不能把 socket 写成功当成 Claude 已接收。用唯一动作标记与新原生 transcript 记录确认 accepted；本地投递索引只恢复手机消息的显示归属，不改原生日志。专用交互进程验证已实际回复，业务会话未注入测试。

附件新增独立 Node workspace 文件传输，Hub 从所选 session 派生 node/cwd，Node 再检查允许根。Go os.Root 防止越界符号链接；8 MiB 单文件限制；上传唯一目录不覆盖用户文件；下载强制 attachment/nosniff，图片仅栅格预览。第一步通过消息中的设备本地路径交给原生工具读取，后续仍需补全 Codex 原生图片 input、消息内输出附件展示和真实两 provider 图文闭环，不能仅以文件 API 测试宣称目标完成。

参考： https://code.claude.com/docs/en/cross-session-messaging ，https://github.com/PeterSR/claude-code-socket-transport （协议说明），https://github.com/siteboon/claudecodeui （行为/调用方式参考）。

## 2026-10-17：真实 native hook child 桥接
直接 peer 投递在用户 bypass 模式下被暂扣，不能外推普通模式测试。改用官方文档允许的 own-child 路径：SessionStart/UserPromptSubmit 的命令启动本程序短生命周期 launcher；launcher 等专用子进程登记后立即完成，避免阻塞原生首次 prompt。子进程从官方 hook 环境取得本 session 的 socket/token；凭据只存在内存，Node 仅向私有 Unix socket 发送带 session/generation 的请求。先验证真实 ancestry、registry、PID/start；每次投递和定时检查目标是否仍为原实例。无权限声明伪造、无 peer key 升权、无 inbound 默认改变；显式 hold/refuse 继续由 Claude 执行。卸载写撤销标记，让运行中的桥接退出。

实测先启动 CLI，再写专用项目 hook，在同一 PID 正常终端输入后注册并收取 provider 消息。最初长驻 async UserPromptSubmit 阻塞输入，已用短 launcher/就绪管道修复；这项失败保留在本地探针日志。原生记录的 origin.peer/isMeta 导致角色变 system，投递确认改为识别明确 peer 来源而非依赖展示 role；助手回显不能冒充收件确认。

## 2026-10-18：通知与会话传输分离

通知改为独立认证的 HTTP 批次，与快照/历史/动作 WebSocket 分开；优先使用小批次、落盘确认和持久游标补发，不依靠给同一 socket 的写锁设置优先级。事件携带可验证的原生身份与范围，消除“必须先收到会话快照”的依赖。Node 状态观察点与事件事务化，控制端入库与去重事务化；推送按订阅并发、按事件去重，测试任务单独处理。手机推送的旧无限重试策略已被下文“实时通知与有限重试”取代；原生动作结果未知时仍禁止自动重发。系统通知显示仍由 Apple/浏览器决定，持久事件不因推送失败被删除。

## 2026-10-18：优先减少传输与重复读取

只优化程序，不调整 Tailscale。复用现有认证、事件与历史接口，取消固定3秒整页读取：当前会话优先、变化触发、空闲30秒兜底、GET合并与取消。增量以单个最新页为单位，不维护另一份持久会话副本或复杂全库同步协议；基准缺失时直接返回完整页。Claude用文件版本跳过未变原生日志读取，Codex在没有可靠版本证据时保留原生读取。压缩成本优先放到静态构建，动态JSON/大WebSocket用快速压缩。

## 2026-10-18：实时通知与有限重试（取代此前积压补推）

改为即时提醒：只推仍未处理的最新事件，首次失败后退避 1/2/4 秒最多重试三次，失败即丢弃。发送前消费游标，重启不恢复已尝试事件；启动跳过积压、普通推送 TTL=0，不让网关为离线手机排队。超过一分钟的延迟事件仅保留收件箱，已处理/已读/被更新事件取代的提醒不再发送。Node 持久传输保留事件记录，不等于恢复后补推系统通知。诊断测试独立 TTL=120 秒。曾考虑 UDP 简化方向，但 PWA 无原始 UDP，Web Push 需要 HTTPS，因此采用有限应用重试，不更换整个协议或削弱动作完整性。

手机连接增加 15 秒空闲心跳与 45 秒接收超时，处理 WebSocket 看似 OPEN 却无响应；隐藏时继续关闭，前台重建。已允许通知的浏览器在应用全局修复登记，不要求打开通知设置页。原生动作回执同样全局只读跟踪，仅找到真实原生收件证据才解除未知状态，不把“终端继续工作”冒充手机消息已接收。
