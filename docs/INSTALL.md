# 安装、运行与维护

以下命令中的 `<hub>` 是 Hub 的 Tailscale DNS 名，`<tailnet>` 是其 MagicDNS 后缀；`<node>` 表示执行设备。尖括号为占位符，运行前替换。本文不含任何真实设备凭据。已验证的原生版本见 [兼容性](COMPATIBILITY.md)；Linux 原生运行、真实手机和锁屏推送不能仅凭本文的操作步骤视为通过。

## 准备与构建

构建需要 Go 1.24 或更新版本，以及符合 Vite 要求的 Node.js 22.12 或更新版本、npm。普通运行只需要生成的 `pocket` 二进制。执行节点另外需要已安装并可用的 Claude Code、Codex 和各自原生登录；需要远程访问的设备及手机安装 Tailscale。纯 Hub 不需要 agent CLI。

```sh
go version
node --version
npm --version
make build
./bin/pocket --version
make check
```

`make build` 安装锁定的前端依赖、编译 PWA 并嵌入 `bin/pocket`。`make check` 运行 Go vet、Go race tests、前端构建及模型测试。`make cross` 生成 macOS arm64、Linux amd64 和 Linux arm64 二进制；交叉编译通过不代表目标系统上的 CLI 或用户服务已实测。原生 probe 脚本会创建真实测试会话，见证据文件，勿将它们当作纯单元测试。

`make release` 在 `bin/release` 生成三平台 `.tar.gz`、`pocket-agents-0.1.0-source.tar.gz` 和 `SHA256SUMS`。二进制包带有 `pocket`、三语 README、公开技术文档、scripts 与依赖许可证；源码包另含 Makefile、Go/前端源码和依赖锁文件，可解压后执行 `make build`。源码包不包含 node_modules、前端构建缓存或应用 data。下载或复制归档后先用 `shasum -a 256 -c SHA256SUMS` 核对完整性；若仅复制单个平台包，只核对 SHA256SUMS 中对应一行。解压后运行 `./pocket doctor`，再依本文安装服务，不需要在目标机器构建前端。

先在执行节点原有的登录终端检查原生环境，再初始化 Pocket：

```sh
claude --version
claude auth status
codex --version
codex login status
codex app-server daemon version
tailscale status
./bin/pocket init --root "$HOME/projects"
./bin/pocket doctor
```

将 `--root` 改为实际存在、允许手机访问的项目根目录。默认 root 为当前用户 home；`--root` 会替换为一个根目录，多根目录请编辑配置的 `roots`。浏览与创建目录都经过真实路径和 symlink 边界检查；创建会话只使用已经存在的目录。

Codex 需使用支持 shared app-server daemon 的原生版本。若原生 daemon 尚未运行，先用原生 CLI 启动，再运行 doctor：

```sh
codex app-server daemon start
codex app-server daemon version
```

Pocket 不替你重启或升级已有 daemon。不同版本应先查看 `codex app-server daemon --help`，确认当前版本提供这些命令；不要为同一 live thread 启动第二个 app-server。Claude 需提供 `--bg`、`agents --json --all`、`attach` 等后台能力。`doctor` 会分别显示版本、原生登录检查、连接及能力，不通过的 provider 不能靠 PWA 页面存在视为可用。

## 单机与私网入口

本机调试可直接运行：

```sh
./bin/pocket both
```

本机开发入口为 `http://127.0.0.1:8787`。`both` 自动建立本机 Node 的应用关联，一个 data 目录只能有一个后台 Pocket 进程。日常手机登录推荐下面的一次性二维码流程。尚未配置 HTTPS 的本机调试可用 `./bin/pocket token`，在登录页展开“使用访问口令登录”并填入；不要把长期 token 拼进 URL、发给 Node 或写进共享日志。

手机使用 Tailscale 私网 HTTPS。在 Hub 先查看现有 Serve 配置，确认所选 HTTPS 端口可供 Pocket 使用，再配置：

```sh
tailscale serve status
tailscale serve --bg --https=443 http://127.0.0.1:8787
tailscale serve status
```

使用 Serve 返回的完整 HTTPS origin，保存为 Pocket `publicUrl` 后重启前台进程或安装服务：

```sh
./bin/pocket init --public-url 'https://<hub>.<tailnet>.ts.net'
./bin/pocket both
```

Serve 仅将服务提供给有权限的 tailnet 成员；Funnel 是互联网公开入口，本部署不需要 Funnel。若 CLI 要求在 tailnet 管理页启用 HTTPS/Serve，由管理员完成后重试。参见 [Tailscale Serve 官方命令说明](https://tailscale.com/docs/reference/tailscale-cli/serve)。设置 public URL 后，浏览器应使用这个 HTTPS 地址，避免以回环地址发起被 Origin 检查拒绝的写请求。Pocket 始终保持回环监听，不改为 `0.0.0.0`。

### 手机扫码登录

Hub 运行且 `publicUrl` 已设为上面的 HTTPS origin 后，在 **Hub 电脑** 运行：

```sh
./bin/pocket login-qr
# macOS：打开 CLI 输出的本机 PNG 路径
open "$HOME/.pocket-agents/login-qr.png"
```

Linux 使用本机图片查看器打开输出路径。已安装二进制可用 `pocket login-qr`；自定义应用目录时带相同 `--data /absolute/app-data`，默认图片存该目录的 `login-qr.png`；也可用 `--output /absolute/private/login-qr.png` 指定输出。CLI 只显示图片路径和到期时间，不打印一次码或长期 owner 口令。

手机先连接允许访问 Hub 的 tailnet，用相机扫描图片并打开链接，页面自动登录。已经登录的电脑/手机还可进入设置，点击“生成登录二维码”给另一部手机扫描。无需把长期 owner token 复制到手机。二维码五分钟内有效且只能使用一次，生成新码会撤销前码；显示过期或已用时在 Hub 重新生成。

二维码图片在本机生成，文件权限 0600；其 HTTPS 链接带独立随机一次码，以 fragment 传入，页面读取后立即移除。服务端验证、一次性消费后设置浏览器登录 cookie。长期 owner token 不出现在二维码中。二维码在有效期内可授予应用访问，仅供自己的设备扫描，不发布或转发到公共位置。Tailscale 连接成功本身不授予 Pocket 管理权限。

扫码仍保留回退：在 Hub 电脑运行 `pocket token`，从登录页展开原口令方式。无法访问时先检查手机 Tailscale、HTTPS 地址及 Hub 服务；不要反复输入已过期的二维码链接。已验证本机 CLI 生成 PNG 及真实 HTTPS 浏览器兑换、fragment 消失、进入概览。具体手机版本、主屏幕 PWA 的会话保留与完整手机体验仍按 [验收记录](ACCEPTANCE.md) 补测。

Hub 角色只运行界面与聚合时，使用 `pocket hub`；远端执行设备使用 `pocket node`。

## 加入第二台执行节点

1. 在 `<node>` 安装二进制与原生 CLI，完成本机登录、`pocket init` 和 `pocket doctor`。
2. 在已登录的 Hub 设备页生成一次性配对码。候选列表只表示 Tailscale 发现，不表示已经接入。
3. 在目标节点终端运行界面给出的命令，或替换下面的占位符：

```sh
pocket pair --hub 'https://<hub>.<tailnet>.ts.net' --code '<one-time-code>'
pocket node
```

配对码 10 分钟失效，只用一次。节点身份与应用 token 保存到本机配置，重启保持；原生 provider 密钥不会随配对上传。已登记节点需要先在 Hub 撤销关联，才能重新配对。撤销后原生任务继续运行，但该 Node 无法再连接 Hub。若要持续运行，停止前台 Node，再安装下面的 `node` 用户服务。

## 配置与原生运行环境

默认应用目录为 `~/.pocket-agents`，可用 `POCKET_HOME` 或普通子命令 `--data /absolute/path` 指定。`config.json` 由首次 init 生成，保留它的 node ID 和配对 token，不复制另一台设备的身份。Hub 地址必须是 origin，不能含路径、查询、fragment 或 URL 凭据。

下面仅展示可编辑的 profile 字段，不要用这个片段覆盖完整配置。路径必须替换为该执行机器上的绝对路径：

```json
{
  "roots": ["/absolute/projects"],
  "profiles": [
    {
      "name": "default",
      "provider": "claude",
      "home": "/absolute/user/.claude",
      "binary": "/absolute/bin/claude",
      "env": ["LANG=en_US.UTF-8"]
    },
    {
      "name": "default",
      "provider": "codex",
      "home": "/absolute/user/.codex",
      "binary": "/absolute/bin/codex",
      "endpoint": "unix:///absolute/user/.codex/app-server-control/app-server-control.sock"
    }
  ]
}
```

`init` 尽可能解析当时 PATH 上的 CLI 绝对路径，并保存 PATH。版本管理器或应用内置 CLI 可能与系统命令不是同一版本，请使用你已登录并验证的路径；更新版本管理器安装位置后同步修改 `binary` 和需要的 PATH。profile 名在同一 provider 内唯一，多个 profile 使用不同原生 home。不要把 home 写成无法展开的 `~`。

Claude 默认 `~/.claude` 保持原生未设置 `CLAUDE_CONFIG_DIR` 的语义；显式改设这个变量会改变部分原生账户/信任配置的位置。Codex endpoint 留空时使用该 profile home 下的共享 socket。对 live execution，profile env 不会改变原有进程配置；Codex 新 thread 也仍由原生 daemon 的既有配置和环境执行。

后台服务不会自动读取 `.zshrc`、`.bashrc` 或当前终端的临时变量。安装 `node` 或 `both` 服务时，Pocket 将安装终端中已有且非空的以下变量捕获到本机 `node-env.json`：

`ANTHROPIC_AUTH_TOKEN`、`ANTHROPIC_API_KEY`、`CLAUDE_CODE_OAUTH_TOKEN`、`ANTHROPIC_BASE_URL`、`OPENAI_API_KEY`、`CODEX_API_KEY`、`CODEX_ACCESS_TOKEN`、`OPENAI_BASE_URL`。

安装输出只列变量名，不显示值。文件权限为 `0600`，仅执行节点使用，Hub API 不传输；纯 Hub 安装不捕获。已存在的服务进程环境优先于文件，显式 profile env 用于相应原生命令。重新安装会合并本次非空值，缺失/空值不会清除旧值。要移除过期值，在本机安全编辑该文件并重启服务；不要输出或提交文件内容。原生钥匙串/原生 home 登录仍由原生 CLI 管理。安装后必须用服务实际创建一条测试会话确认环境，终端登录成功不足以证明后台服务可推理。

## macOS / Linux 用户服务

在有正确 PATH 与原生环境的终端运行，`--role` 可选 `both`、`hub` 或 `node`：

```sh
./bin/pocket service install --role both
"$HOME/.local/bin/pocket" service start
"$HOME/.local/bin/pocket" service status
```

安装把二进制复制到 `~/.local/bin/pocket`，只创建当前用户服务，不需要 root。将 `~/.local/bin` 加入终端 PATH，或继续用完整路径。安装本身不启动服务；从前台模式切换时先 Ctrl+C 退出该 Pocket 进程。运行中的原生持久会话不会因此被主动停止。

| 平台 | 用户服务配置 | 启动/日志 |
| --- | --- | --- |
| macOS | `~/Library/LaunchAgents/local.pocket-agents.plist` | `launchctl` 用户 GUI 域；`~/.pocket-agents/logs/service.log` |
| Linux | `~/.config/systemd/user/pocket-agents.service` | `systemctl --user`；`journalctl --user -u pocket-agents.service` |

自定义 data 时，配置和 macOS 日志位于所选目录；服务文件会记录这个绝对路径。macOS 使用 LaunchAgent，要求用户登录图形会话；Linux 使用现有 user systemd manager。若需要 Linux 注销后继续运行，应由系统管理员按环境配置 user lingering，本文不自动改变机器登录策略。Linux unit 使用 `KillMode=process`，避免停止 Pocket 时连带杀掉原生后台子进程；Linux 真机生命周期仍需验收。

```sh
pocket service restart
pocket service stop
pocket service start
pocket service status
```

macOS 上 `restart` 对应 `launchctl kickstart -k`；`stop` 卸载当前 LaunchAgent，后续 `start` 再 bootstrap。改服务角色、PATH 或 data 路径时应先 stop、重新 install，再 start，不能仅 kickstart 旧配置。Linux 修改 unit 后也通过 stop/install/start 完成 daemon-reload。

macOS 已真实验证：通过本节 `service restart` 重启 Pocket 后，Claude 原生后台实例和 Codex shared daemon 中的测试会话均保留相同 native ID、相同 execution ID，并可继续连接。该结果不涵盖系统重启、原生 daemon restart/update 或未完成的启动对话。

### macOS 文件夹权限

LaunchAgent 的文件权限与交互终端不同。Documents、Desktop 等受保护目录可能在原生 `getcwd` 或启动阶段等待系统授权，表现为创建超时、`unknown`。先使用明确可访问的项目目录完成服务验证；需要受保护目录时，在“系统设置 → 隐私与安全性 → 文件与文件夹”检查实际后台程序的权限，必要时根据系统提示授予相应访问，再重启 Pocket 服务并复测。不要自动扩大为全盘访问，也不要把超时当作确定未创建而连续重发。

Claude 首次目录信任和 macOS TCC 是两层不同权限。“原生启动对话”显示 Claude 自己的信任提示，使用方向键/Enter 选择，允许取消；它不会代替 macOS 文件夹授权。已在真实 PWA 的桌面浏览器从新未信任目录完成信任、原生创建、自动进入会话并收到回复；真实手机操作仍待实测。回退路径是在目标目录使用已配置的原生 Claude binary 完成原生交互信任后再创建；不编辑原生信任 JSON、不绕过批准策略。

## 日常终端接入与回退

先直接使用可控的启动入口：

```sh
pocket claude
pocket codex
```

Claude 启动原生后台会话并自动 attach；Codex 把官方 TUI 连接到 profile 对应的 shared daemon。这样后续日常终端新建的会话可被 Node 自动发现，手机连接同一运行实例。普通未接入的 Claude 交互进程、其他私有 Codex writer 只读，不能用新开实例冒充控制现有实例。

需要保留 `claude`、`codex` 两个日常命令名时，先查看输出再应用到当前 bash/zsh：

```sh
"$HOME/.local/bin/pocket" shell-init
eval "$("$HOME/.local/bin/pocket" shell-init)"
```

确认后，将上述 `eval` 行放入你使用的 shell 启动文件一次。该命令仅输出两个函数，不自动修改 shell 文件或原生 hooks/settings。它调用已安装 Pocket 的绝对路径，再由配置的绝对原生 binary 执行，避免递归调用包装函数。`POCKET_PROFILE` 选择非默认 profile；自定义应用目录时先 `export POCKET_HOME=/absolute/path`，因为 `pocket claude/codex` 后的参数全部属于原生 CLI，不支持额外 Pocket `--data`。

Claude 明确的 `--resume <native-id>` 若指向已运行后台实例，会 attach 到该实例；若已停止，则使用原生恢复。Codex 可以使用 `pocket codex resume <native-id>`。页面中的原生终端入口给出当前会话的具体连接方式。原生管理与非交互行为仍以实际 CLI 版本支持为准。

回退时删除 shell 文件中的该 `eval` 行，并在当前 shell 执行：

```sh
unset -f claude codex
```

新终端将恢复 PATH 上原有原生命令。也可临时直接运行原生 binary 的绝对路径；已存在的原生历史和任务不受 shell 函数删除影响。

## 关注项目与默认会话列表

概览的“管理关注项目”进入设置：选择执行设备、输入该设备上的项目绝对路径、点“添加项目”。所选目录及其子目录纳入共享范围；同一 Hub 的手机和电脑看到相同配置。新建页优先使用当前设备的关注项目，并提供快捷选择。关注范围是显示与通知偏好，不能扩大节点 `roots` 的目录访问权限。

概览默认显示活跃主会话。“活跃”表示原生实例仍存活，“正在工作”还需要原生本轮工作状态；状态未知不冒充正在工作。需要回答/审批只包括当前实例的真实等待，已完成未读不计入，子 agent 不占默认主列表。原生已退出的旧等待会清除 pending 并显示 unknown。外部独占 writer 可显示实例存活，但 execution 不可验证、控制全部禁用。

实例状态未知的会话单列“运行状态待确认”，不混入已退出历史。历史记录继续自动发现，在历史筛选中选择已有会话复用；点击项目标签后，活跃与历史列表都按选中项目筛选。“临时查看所有目录”只用于概览临时查找；需要长期改变设备树、最近会话、收件箱与推送范围时，在设置增减关注项目或改为“所有已发现的目录”。移除关注不会结束会话或删除原生历史，也不补发此前因范围而跳过的通知。


## 手机安装与通知验证

手机连接 Tailscale 后，扫描 Hub 生成的一次性二维码登录私网 HTTPS，再使用浏览器“安装应用”或“添加到主屏幕”。从 PWA 的设置/通知入口主动授予权限。iPhone/iPad 的 Web Push 要求从主屏幕 Web App 发起权限请求，平台支持从 iOS/iPadOS 16.4 起提供；具体设备仍需实测。[WebKit 官方说明](https://webkit.org/blog/13878/web-push-for-web-apps-on-ios-and-ipados/)

应用侧消息推送默认开启，由当前设备的系统通知权限决定是否接收，不需要另开一个应用开关。进入“通知”或“设置”：未授权时点“允许系统通知”；允许后自动登记当前设备；之后每次回到前台或连接恢复都会检查并修复登记，无需反复打开设置页。若系统已拒绝，按页面指引到系统设置重新允许；返回应用会复查。网页无法读取专注模式、定时摘要或锁屏显示选项，因此不能仅凭“设备已登记”证明锁屏显示。

普通提醒只推仍未处理的最新事项：失败后按 1/2/4 秒最多重试三次，再失败即丢弃；手机离线期间不积压补推，通知收件箱仍保留记录。系统权限开启不代表手机实际显示。

Apple 返回 HTTP 403 / BadJwtToken 时检查控制端推送身份，无需反复授予手机权限。旧默认 localhost 地址会自动使用 publicUrl；也可在私有 config.json 的 pushSubject 配置实际邮箱或 HTTPS 联系地址。保留已有 VAPID 密钥，否则现有订阅可能不再匹配。

登记后点“发送锁屏测试通知”，随即锁屏，通常约 10–30 秒后收到。测试只发给当前设备，不创建或操作原生会话；点击回到设置，可查看服务接受、设备接收、显示请求与点击结果。结果未知不自动重发，每台设备的测试限频一分钟。此测试通过后，还需验证真实任务完成/待处理通知及正确会话跳转。

Hub 必须持续运行并能出站连接浏览器的推送服务。Tailscale、应用登录、浏览器通知权限是不同条件。验证时记录手机型号、OS、浏览器版本：创建真实待审批或完成事件，锁屏/关闭 PWA，确认系统收到通知，点击后进入正确会话；同时测试中文输入法、键盘、安全区、草稿刷新、深链接和返回。当前真实 Android 及其他手机的结果应在验收记录中填写，桌面 390×844 模拟测试不能替代。


## 手机断线与恢复

如果 Safari 显示 `Response served by service worker has redirections`，这是缓存页面的重定向兼容问题，不能据此判断控制端已掉线。旧服务器会把 `/index.html` 重定向到 `/`，Service Worker 缓存的响应仍带有重定向记录，Safari 可能拒绝用它打开页面。修复版直接提供首页，并兼容旧缓存；更新后台和 UI 后，关闭主屏幕应用再重新打开，让浏览器更新 Service Worker。若仍停在错误页，可在原控制端地址后加 `/?pocket_update_check=recover` 打开一次，绕过旧版页面缓存并触发更新，然后重新打开主屏幕应用。此入口不绕过登录，也不会清除草稿、通知订阅或网站数据。Safari 与主屏幕应用可能使用独立的网站存储，Safari 能打开不等于主屏幕应用的旧缓存已更新；真机恢复结果仍需在出现问题的应用中确认。

iPhone 锁屏或切到后台时可能挂起 PWA，不能依赖后台 WebSocket 常驻。Pocket 在页面隐藏时关闭事件连接并暂停重试；回到前台、网络恢复或浏览器恢复缓存页面时重建连接，保留事件游标并刷新当前对话。连接建立超过 12 秒会重试，失败间隔由 1 秒逐步增加到最多 15 秒。空闲时控制端每 15 秒发送小心跳，前台连续 45 秒未收到任何数据会主动重连；对话页也提供“立即重连”。通知仍由独立 Web Push 通道投递，不依赖这个页面连接；掉线恢复不会补发过时通知。网络错误保留登录与草稿，只有实际未认证才回登录页。

若 Tailscale 显示已连接而 Pocket 仍不可达，先确认控制端电脑没有睡眠、Pocket 服务仍运行。手机 VPN 已连接不代表控制端在线。需要长期远程使用的 Mac，可在系统电池/电源设置中启用接电时显示器关闭仍防止自动睡眠；合盖行为须另外确认。Pocket 不自动更改系统睡眠或 VPN 设置。

## 升级、备份与卸载

独立通知通道版本须先升级控制端，再升级其他执行设备。旧执行设备仍可使用 WebSocket 上报事件；新执行设备在旧控制端不支持 `/api/node/events` 时保留未确认事件，待控制端升级后补齐收件箱记录；过时记录不会补推到手机。无需重新配对或重新授予手机通知权限。

升级只替换 Pocket 自身二进制和用户服务。先按下文保存应用备份；macOS 本机开发、默认数据目录且已安装 both 角色时，使用 `make deploy-local` 构建、复用持久签名、安装并重启服务，同时同步 UI。只有前端变化时使用 `make deploy-ui`，不重启后台。

更改角色、数据目录或服务环境时，先构建；macOS 本机开发还须运行 `make sign-local`。然后完整 stop/install/start，以便加载新的服务配置：

```sh
pocket service stop
./bin/pocket service install --role both
"$HOME/.local/bin/pocket" service start
"$HOME/.local/bin/pocket" doctor
```

按实际角色替换 `both`，自定义 data 时在 install 加相同 `--data`。这不会升级 Claude、Codex 或 Tailscale；原生软件升级单独安排，尤其不要在活任务期间顺手重启 Codex daemon。手机静态资源使用构建内容生成缓存版本；发布后重开/刷新 PWA 并核对新版本。

备份时停止 Pocket，确认进程退出，将整个应用 data 目录复制到受保护的备份位置；SQLite 使用 WAL，服务未停时不能只复制主 `.sqlite` 文件。应用备份含 owner/node token、捕获的本机环境、动作文本、元数据和推送密钥，应与原生凭据同等保护。原生 Claude/Codex home、Claude 相关默认配置和原生钥匙串需要独立按原生方案备份，Pocket 数据库不替代它们。恢复同一节点身份前先确保旧节点不再同时运行；给新设备接入时重新 init/pair，不克隆旧节点身份。

单纯重建会话索引应在可丢弃测试环境验证：停止 Pocket、备份 Hub 数据库，仅清空 `hub.sqlite` 的 `sessions` 表，再启动并等待 Node 重新发现。若使用系统 `sqlite3`，可执行 `sqlite3 /absolute/test-data/hub.sqlite 'DELETE FROM sessions;'`。不要删除 `devices`、`metadata`、`actions`、`kv` 或整个 Node 数据库；否则会丢失关联、用户偏好、确认记录和事件游标。原生历史不参与这个删除操作。

卸载用户服务：

```sh
pocket service uninstall
```

它停止并删除本项目的 LaunchAgent/systemd unit，保留 `~/.local/bin/pocket`、应用 data、原生 CLI、原生日志和持久任务。确认备份与回退需求后，才手动移除 Pocket binary 或其明确的应用目录；不要删除原生 home。另行删除 shell-init 行。若本机 443 Serve 配置仅用于 Pocket，可用 `tailscale serve --bg --https=443 off` 撤下这条入口；先检查 `tailscale serve status`，保留启用时的其他 flags，不要用 reset 清除同机其他服务。

## macOS 文稿权限与仅更新界面

`make deploy-local` 使用持久的本机开发签名身份，升级时复用相同证书；安装器会拒绝不符合已安装签名身份的替换。单独执行 `go build` 或 `make build` 不包含这一步，临时签名仍可能使 macOS 无法沿用之前的文稿访问授权。首次迁移签名身份可能需要用户重新授权，详见 [macOS 签名说明](MACOS-SIGNING.md)。不要通过修改 TCC 数据库或给程序全磁盘访问来处理普通界面更新。

安装支持 `ui install` 的后台版本后，纯前端修改使用：

```sh
make deploy-ui
# 或将可信构建复制到 Hub 后：
~/.local/bin/pocket ui install --from /absolute/path/to/web/dist
```

该命令验证前端协议和必需资源，将完整版本写入私有 data 下的 `web-ui/releases`，原子切换界面版本，不替换后台可执行文件、不重启服务。旧哈希资源保留供已打开页面使用；页面刷新或 PWA 更新后使用新版本。只安装自己信任的构建，前端代码拥有当前用户的网页访问权限。自定义 data 部署须同时传 `--data`。没有外部界面版本时自动使用二进制内嵌界面。

Go 后台代码变更需构建、使用原身份签名、安装并重启；保留本机签名材料，不要每次更新重新生成。公开发行仍需 Developer ID 和公证，本机开发签名不能替代。后台协议变更时应同步安装匹配的前端；只有确认内嵌界面与当前后台兼容、并备份当前指针后，才可通过移除 data 内的 `web-ui/current` 指针恢复内嵌界面，无需重启。

#### 新建 Codex 会话的审批策略

Codex profile 可显式设置 `"codexCreateWithoutApproval": true`。Pocket 新建线程时会传入 `approvalPolicy: "never"` 和 `sandbox: "danger-full-access"`，使新线程使用无需逐项批准、完整访问权限的模式。省略或设为 false 时沿用原生默认值。此选项不修改已有线程，也不会自动处理当前待审批请求。Claude 的对应新建选项仍是 `claudeCreatePermissionMode: "bypassPermissions"`。

如需旧 Codex 会话后续轮次也采用免审批模式，可设置 `"codexWithoutApproval": true`。此选项同时覆盖新建线程和通过 Pocket 发送的 `turn/start`（`approvalPolicy: never`、`sandboxPolicy.type: dangerFullAccess`）。正在执行的旧轮次和已有待审批请求不会被自动批准；从下一次发送开始生效。
