# 第三方依赖

原创部分使用根目录 PolyForm Noncommercial 1.0.0 License；第三方依赖保留各自许可证，不受本项目重新许可。精确版本由 go.mod/go.sum 与 web/package-lock.json 固定。

| 依赖 | 用途 | 许可证 |
| --- | --- | --- |
| github.com/gorilla/websocket | Node/Hub 与 Codex Unix WebSocket | BSD-2-Clause |
| modernc.org/sqlite 及 libc/memory/mathutil | 无 C 工具链 SQLite | 各模块 LICENSE（SQLite 核心 public domain，Go 端 BSD 系列） |
| github.com/creack/pty | 官方 Claude attach / 启动终端 | MIT |
| github.com/SherClockHolmes/webpush-go | 标准 Web Push 加密与发送 | MIT |
| github.com/skip2/go-qrcode | 本机一次性登录二维码 PNG 编码 | MIT |
| golang.org/x/crypto, x/sys, x/term, x/exp | 通用底层支持 | BSD-3-Clause |
| React / React DOM | PWA UI | MIT |
| Vite / TypeScript / esbuild | 仅前端构建工具 | MIT / Apache-2.0 / MIT |

`make licenses` 生成实际已锁定 Go/npm 包许可证副本到 `bin/licenses/`。发行二进制时一并提供 LICENSE、THIRD_PARTY.md 和该目录。开发工具依赖与间接依赖的原始声明也应保留。

运行前提为系统 Tailscale 与官方 Claude Code/Codex（各自单独许可、安装、登录）；它们不被本项目打包或重新许可。

Markdown 渲染依赖 react-markdown / remark-gfm（MIT），传递依赖按锁文件及发布 notices 保留许可。
