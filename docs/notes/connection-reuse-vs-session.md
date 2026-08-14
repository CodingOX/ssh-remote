# 连接复用 vs Session

本文是当前真相说明，不是实现计划。决策见 [ADR-0003](../adr/0003-no-session-reuse-daily-ops.md)。

产品定位是**日常排障**：`hosts` → 只读探查 → 看日志 / 拉小文件 → 必要时白名单 `put` 再 `exec`。这类工作流是多次独立命令，不依赖远端 shell 记住上次的 `cd`。

## 三层不要混用

| 层 | 复用的是什么 | 本项目 |
| --- | --- | --- |
| Transport / **连接复用** | TCP、SSH 握手、认证、跳板 | **有**。`ControlMaster=auto` + `ControlPersist=60s` |
| 进程 | 本机长驻进程持有句柄 | **无**。每次 `ssh-remote` 都是新进程 |
| **Session** | 同一条远端交互 shell 的 cwd / env / 变量 / 交互程序 | **无**。每次 `exec` 新开 channel + 新非交互 shell |

当前实现只到第一层。runner 对 ssh/scp 传：

```text
ControlMaster=auto
ControlPersist=60s
ControlPath=<cache>/ssh-remote/cm-%C
```

同 Host、约 60 秒内的第二次调用不再握手，只新开一条 SSH channel。远端仍然是「新起一个非交互 shell → 跑完退出」。Agent **不必**显式管理任何 session。

## 和 MCP 的关系

MCP **能做成** Session，但 MCP **不等于** Session。

- CLI 每次 fork 即结束，进程里存不住 PTY / channel 句柄，所以做不成 Session。
- stdio MCP Server 跟着宿主活，**有资格**在进程内持有一条远端 shell。要真正复用 Session，还得自己实现打开 / 写入 / 关闭、租约和隔离。
- stdio 活不过「这一次 Agent 对话」。跨对话续上需要独立 daemon，那是另一条产品线，不是给现有 CLI 加 `--mode mcp`。

评审过「一套代码两种 mode」的草案：**不通过**。主要原因是：

1. 与已接受的 ADR-0001（非 MCP、非长驻会话）冲突。
2. 不能覆写 S2 CLI 合同；MCP 也不得绕过现有 policy。
3. 「连接池 / 长连接」不能冒充 Session；SQL/Redis 连接池模型也不属于本项目。

## 性能：日常排障差在哪

对 `df`、`uptime`、`tail -n 100`、`systemctl status` 这类排障命令：

| 阶段 | 冷启动（无 mux） | 现在（mux 热） | 真 Session |
| --- | --- | --- | --- |
| TCP + 握手 + 认证 + 跳板 | 200ms ~ 2s+ | ~0 | ~0 |
| 再起本机 CLI / ssh | 每次都有 | 数毫秒到十几毫秒 | 0 |
| 新开 SSH channel | 含在握手里 | 约 1 个 RTT | 0 |
| 远端再起非交互 shell | 每次都有 | 视 bashrc，约 20~200ms | 0 |
| 命令本身 | 一样 | 一样 | 一样 |

`ControlMaster` 已经拿掉绝大多数连接开销。真 Session 再省的，多半是「新 channel + 新 shell」那几十到一两百毫秒。命令一跑过 200ms，体感几乎没有。

Session 真正多出来的是**状态**，不是吞吐。日常排障不需要这份状态：工作目录用 `--workdir` 或绝对路径；连续操作靠多次 `exec`；大输出用 `get`，不要挂着 `tail -f`。

## 结论

- 保持 Skill + 薄 CLI；连接复用交给系统 OpenSSH。
- 不引入 MCP，不做交互 PTY / 长驻 Session。
- 若以后要 Session，单独立项并另写 ADR，不在本工具上叠第二种运行模式。
