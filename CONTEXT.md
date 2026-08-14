# SSH Remote Agent

本机 AI Agent 通过 Skill + 薄 CLI，复用系统 OpenSSH 对远程主机做排障与有限文件操作的领域语境。

## Language

**Host 别名**：
`~/.ssh/config` 中的 Host 名，是 CLI 唯一的主机寻址方式；认证、跳板、端口均委托给系统 OpenSSH。
_Avoid_: 连接串, profile 名（本项目不自建主机账密库）, MCP server 名

**有限写（模式 α）**：
允许向远程**路径白名单**内上传文件；上传后 Agent 仍可执行命令，不要求人类终审生效。
_Avoid_: 暂存区人终审, 剧本库, 任意路径 SFTP

**写路径白名单**：
远程侧允许 `put` 的路径集合（如 `/tmp/**`、远端 `$HOME/agent-drop/**`），用于防止覆盖系统关键路径。
_Avoid_: 上传目录（易被理解成唯一工作区）, staging only

**命令黑名单**：
在 CLI 层强制拒绝的高危命令模式；不依赖模型自觉。
_Avoid_: 审批流, 角色策略矩阵

**薄 CLI**：
对系统 `ssh`/`scp`/`sftp` 的封装，负责策略强制与结构化输出；不自建 SSH 协议栈。
_Avoid_: MCP Server, ssh2 自建连接层

**运维 Skill**：
指导 Agent 何时、对何 Host、用何子命令排障的说明与约定；不替代 CLI 的安全强制。
_Avoid_: MCP Tool, 配置助手 skill（仅写 mcp.json 的那种）

**连接复用**：
同一 Host 上多次 CLI 调用复用 TCP / SSH 握手（OpenSSH ControlMaster）；每次 `exec` 仍是新 channel、新非交互 shell。
_Avoid_: Session, 长连接, 长驻 MCP

**Session**：
同一条远端交互 shell 上的状态（工作目录、环境变量、交互程序）。本项目不提供。
_Avoid_: 连接复用, ControlMaster
