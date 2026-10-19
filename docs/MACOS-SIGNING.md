# macOS 更新与文件权限

临时签名的 Go 构建使用随内容变化的 cdhash 作为指定要求，后台更新会使 macOS 文稿授权失配。不要用反复授权、修改 TCC 数据库、关闭 SIP 或授予终端完全磁盘访问来掩盖它。

本机开发部署使用 `python3 scripts/sign-macos-local.py bin/pocket`。脚本首次在 `~/.pocket-agents-signing` 创建专用签名证书和私钥钥匙串，后续复用；固定 identifier `local.pocket-agents`，指定要求同时绑定证书指纹。签名前临时加入该钥匙串搜索路径，finally 恢复原搜索路径；不改变系统证书信任设置。目录0700、钥匙串和密码文件0600。不要删除或将该私有目录提交、上传或分发。私钥仅用于本机开发签名，不是 Apple Developer ID。

`make deploy-local` 构建、签名、安装并重启。安装器校验候选满足已安装证书签名的指定要求；拒绝降回临时签名或更换证书，且在替换文件前检查。首次从临时签名迁移时 macOS 仍需要用户批准一次；不能自动批准或继承另一签名身份的授权。

跨设备应各自在本机生成开发身份，不复制私钥。公开发布必须使用稳定 Apple Developer ID、适当公证和一致安装身份；此脚本不解决新用户 Gatekeeper 分发验收。证书期限十年，丢失密钥或更换证书需要重新迁移授权。

验收要求：两个不同构建、同指定要求；同身份升级允许、无签名升级拒绝；用户批准初始身份后，通过真实服务上传/读取文稿中的附件，再安装另一构建和重启重复操作；对比 TCC prompting 日志。未获得最后一项证据前不能宣称反复授权已解决。

依据：[Apple TN3127](https://developer.apple.com/documentation/technotes/tn3127-inside-code-signing-requirements)、[Code Signing Tasks](https://developer.apple.com/library/archive/documentation/Security/Conceptual/CodeSigningGuide/Procedures/Procedures.html)。
