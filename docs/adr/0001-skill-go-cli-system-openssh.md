# Skill + Go 薄 CLI 调用系统 OpenSSH（非 MCP、非内嵌 SSH 栈）

本机 Agent 需要远程排障与有限文件能力，但不走 MCP。决定做成「运维 Skill + Go 实现的薄 CLI」：CLI 用子进程调用系统 `ssh`/`scp`/`sftp`，Host/认证/ProxyJump 全部复用 `~/.ssh/config`；策略（命令黑名单、写路径白名单、输出与文件大小截断）在 CLI 内强制。日后可用 NPM 包仅作跨平台二进制分发壳，运行时仍是 Go 二进制而非 Node SSH 实现。

**Status:** accepted

**Considered Options:**

- MCP Server（ssh-mcp / ssh-mcp-server 形态）— 与「不要 MCP、Skill 优先」冲突，常驻与工具面过重
- Skill 直接拼裸 `ssh` 命令 — 策略无法强制，输出不稳定
- Go/Node 内嵌 SSH 库 — 重复实现 config/跳板，违背薄封装
- Python + uv CLI — 个人安装顺，但单二进制与日后 NPM 包装分发更弱

**Consequences:**

- 必须依赖本机系统 OpenSSH 客户端（`ssh`、`scp`）；运行前通过 `ssh-remote doctor <host>` 的 `ssh_binary` 与 `scp_binary` 字段确认
- 仅支持 macOS 与 Linux；不支持 Windows 原生环境，WSL 按 Linux 环境处理
- JSON 输出合同与 exit code 语义成为 Agent 集成的稳定面，换语言或加 NPM 壳时不应破坏
- 不提供交互 PTY / 长驻会话；有状态工作流由多次 `exec` 与远端路径约定完成
