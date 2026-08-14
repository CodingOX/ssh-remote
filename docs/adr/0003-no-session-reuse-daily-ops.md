# 保持 CLI + 连接复用，不做 MCP / Session

本工具定位是**日常排障**：对已配置 Host 跑几次只读探查、拉小日志、白名单内放脚本。决定继续走「运维 Skill + 每次独立的 Go 薄 CLI」，连接层只保留 OpenSSH `ControlMaster` 握手复用；**不**做 MCP Server，**不**做交互 PTY / 长驻 shell Session。有状态工作流仍靠多次 `exec` 与远端路径约定完成。

**Status:** accepted

**Considered Options:**

- **MCP Server 持有长驻进程，进而做 Session 复用** — MCP 只提供「进程能活着」的资格，Session 仍要自建 PTY/租约/隔离；stdio 活不过宿主对话，跨对话还得单独 daemon。和 ADR-0001「非 MCP、Skill 优先」冲突，常驻面与安全边界对日常排障过重。
- **一套代码同时提供 CLI 与 MCP 两种 mode** — 看起来复用核心，实际是两套生命周期、两套策略入口。评审结论：MCP 路径极易绕过现有 CLI 强制策略；也容易把「连接池 / 握手复用」误写成 Session。
- **继续现状：CLI 子进程 + ControlMaster**（采纳）— 排障命令本身通常比握手贵；mux 已拿掉绝大多数连接开销。Agent 用法不变，策略仍在每次进程里强制生效。

**Consequences:**

- 每次 `exec` 仍是新 SSH channel + 新非交互 shell：`cd`、环境变量、交互程序状态**不会**跨调用保留。
- 同 Host、约 60s 内的多次调用只复用握手，不复用 Session；不要把 `ControlMaster` 称为 Session。
- 若未来真要交互 Session / `tail -f`，视为新产品边界，须另开 ADR 并重做安全模型；不能当成 S2 的「第二种 mode」。
- 分析与分层定义见 `docs/notes/connection-reuse-vs-session.md`。
