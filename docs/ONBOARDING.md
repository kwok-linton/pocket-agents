# 安装与接入体验

## 产品流程

官网负责说明、下载、发布版本与安装入口。用户自己的 Hub 网页负责私网设备、手机登录、二维码、原生会话；官网不保存用户 Agent 会话或长期令牌。首版仍是单人自托管，不引入 SaaS 注册账户。

1. 控制端普通用户安装对应二进制，运行 `pocket setup`（默认 both，服务器仅当 Hub 可用 `--role hub`）。安装用户服务并打开 `/setup`。系统不重置 CLI 登录或更换 Tailscale 账号。
2. 新签名第一次访问受保护目录仍由用户批准。已签名后更新须使用同一身份，见 MACOS-SIGNING.md。
3. 本地网页通过 CLI 请求五分钟一次性登录，不在 URL 放 owner token。`pocket open` 重新打开向导。无桌面主机输出一次性 loopback 链接，用户通过自己的 SSH 端口转发访问，不开放8787公网端口。
4. 向导检测 Tailscale。未安装/未登录提供官方安装登录入口；已连接显示状态。未配置 HTTPS 时提供当前主机的 Serve/init/restart 命令，须先检查已有Serve映射。账号认证、系统权限、网络管理员启用HTTPS由本人完成，当前尚未提供网页自动安装Tailscale或自动改Serve的功能。
5. 网页生成手机二维码。手机先加入同一tailnet；iPhone Safari添加到主屏幕后开启通知，Android Chrome安装。二维码过期后显式重新生成，旧码撤销。
6. Mac/Linux节点：安装二进制、CLI和Tailscale，在网页生成一次性pair指令，目标机器执行并安装node服务。现有设备状态与原生CLI诊断来自真实API。树莓派须64位Linux；Linux用户systemd/linger需管理员按环境配置。32位ARM及Windows原生不支持，Intel Mac目前无发布包。
7. 在设置发现并显式关注项目，不把所有历史子目录自动铺到首页。回到控制台使用日常会话。

## 浏览器管理

`/setup` 展示每个有效cookie登录的名称、浏览器报告类型、创建/最近使用/到期信息及当前标记。可改名、撤销。撤销立即拒绝新的HTTP请求，事件WebSocket每轮校验（最长约1秒后停止后续事件）；已启动的原生动作不会被中断。无需重新配对执行节点。旧登录缺少类型，标作未知；首次再次使用时登记。一个物理手机的Safari/PWA可能是不同登录，UA不是硬件认证。当前所有扫码登录均为owner权限，没有团队/角色模型。

当前撤销的是网页登录，不是Tailscale网络成员或独立推送订阅；推送管理仍在通知设置。历史已投递推送无法回收。产品后续需要将订阅绑定登录身份，才能提供完整“移除此手机”的语义；当前按钮准确命名为“撤销登录”。

## 发布安装脚本

`scripts/install.sh` 支持 `--base-url`、`--version`、`--role`、`--hub`、`--code`、`--no-open`。HTTPS下载、SHA256核对、精确提取二进制；非root安装至~/.local/bin。Mac必须通过codesign和Gatekeeper验证，不能用开发自签包冒充公开发行包。Linux先检查用户级systemd。

`package-release.py` 将install.sh连同SHA256SUMS放入release目录。发布者须上传版本包、脚本及校验文件到真实HTTPS源，才可在官网给出curl入口。未配置发布地址时脚本明确失败，不猜测GitHub仓库或虚构域名。

官网最终命令形式（下面是占位示例，尚不可直接使用）：

```sh
curl -fsSL https://YOUR_RELEASE_HOST/v0.1.0/install.sh -o /tmp/pocket-install.sh
sh /tmp/pocket-install.sh --base-url https://YOUR_RELEASE_HOST/v0.1.0
```

执行节点可追加 `--role node --hub HTTPS_HUB --code ONE_TIME_CODE`。配对码不要放公共文档；仅在私有控制台生成。当前官网/Release托管地址、Apple Developer ID和公证未配置，因此不能宣称公开一键安装已完成。

## 验证范围

自动化测试不能代替首次安装和真实手机验收。平台覆盖与待验证项目见 [验收清单](ACCEPTANCE.md)。

参考：[Tailscale安装](https://tailscale.com/docs/install)、[Serve](https://tailscale.com/docs/features/tailscale-serve)。

### Mac 日常打开入口
控制端 service install（hub/both）现在安装 ~/Applications/Pocket Agents.app。
双击应用调用固定的本机 pocket open --data 配置目录，打开一次性登录后的本地 /setup。
登录页提供 pocket-agents://open 唤起入口，不再把 login-qr 命令作为主要路径。
URL 参数不会传给 CLI，不能指定命令、数据目录或跳转地址。
2026-10-17 已实测启动应用后 Chrome 显示已登录 /setup 与二维码生成按钮。
内置浏览器的自动化安全策略阻止了自定义协议导航，因此网页点击唤起尚未端到端确认；
用户可直接双击已安装应用。正式分发仍需应用签名/公证及公开安装包。
