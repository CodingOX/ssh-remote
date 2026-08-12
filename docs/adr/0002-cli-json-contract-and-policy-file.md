# CLI 统一 JSON 合同 + 独立 policy 文件

Agent 与 CLI 的集成面定为：每次调用 stdout 输出**单个 JSON 对象**（成功或失败皆然），并用进程退出码区分失败类（用法错误 / 策略拒绝 / 远端命令非零 / 连接与超时等）。策略不写入 `~/.ssh/config`，而放在用户级 `~/.config/ssh-remote/policy.toml`（缺失则用内置安全默认），与 Host 寻址职责分离。

**Status:** accepted

**Considered Options:**

- 默认人类文本 + 可选 `--json` — Agent 易漏旗标，解析不稳
- 策略写进 ssh config 自定义指令 — 污染用户 ssh 配置、可移植性差
- 仅内置不可配默认 — 写白名单难以按人调整

**Consequences:**

- Skill 必须规定「以 JSON 为准」，不要解析人类日志格式
- policy 变更无需改 Host 配置；Host 增删仍只维护 `~/.ssh/config`
