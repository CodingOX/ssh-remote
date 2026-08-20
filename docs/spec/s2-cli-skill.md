# Spec：SSH Remote S2（Skill + Go 薄 CLI）

**Status:** draft-for-implementation  
**Based on:** Grill outcome（已确认）+ ADR-0001 + ADR-0002 + ADR-0003
**Scope:** S2 可验收合同；不含实现细节与 NPM 分发壳

---

## 1. 目标与非目标

### 1.1 目标

本机 AI Agent 通过 **运维 Skill** 调用 **Go 薄 CLI**，在已配置于 `~/.ssh/config` 的远程主机上：

1. 发现可用 Host 别名  
2. 执行远程命令并拿回结构化结果  
3. 下载小文件 / 上传到写路径白名单内  
4. 在 CLI 层强制：命令黑名单、写路径白名单、输出与文件大小截断、超时  

### 1.2 非目标（S2 明确不做）

| 非目标 | 说明 |
| --- | --- |
| MCP Server / MCP Tools | 不提供、不兼容 MCP 传输 |
| 交互 PTY / 长驻 shell 会话 | 无 `open-session`；状态靠多次 `exec` 与远端路径 |
| 自建主机账密库 | 不存 password/key；认证只走系统 OpenSSH |
| 内嵌 SSH 协议栈 | 不使用 `crypto/ssh` 等替代系统客户端连目标机 |
| 角色×环境策略矩阵 / 人机审批流 | 不做 ssh-mcp 级 policy engine |
| 模式 β 人终审暂存区 | 有限写 = 模式 α（白名单内可 put，随后可 exec） |
| NPM 分发壳 | 二期；本 Spec 不定义 package.json |
| Windows | 不支持；不构建或发布 Windows 二进制。WSL 内的 Linux 环境按 Linux 处理 |

---

## 2. 架构与职责边界

```text
[ 人类 ]
   │
   ▼
[ Agent + 运维 Skill ]     触发时机、排障流程、如何读 JSON、软约定
   │  子进程调用
   ▼
[ ssh-remote CLI (Go) ]    参数校验、策略强制、截断、拼装 JSON、exit code
   │  exec
   ▼
[ 系统 ssh / scp / sftp ]  Host 别名、认证、ProxyJump、已知主机
   │
   ▼
[ 远程主机 ]
```

| 层 | 必须负责 | 禁止假设 |
| --- | --- | --- |
| Skill | 教 Agent 何时调用、如何解释字段、排障步骤 | 不可作为唯一安全边界 |
| CLI | 黑名单/白名单/限额/超时/JSON/exit | 不解析业务日志语义 |
| OpenSSH | 连通与认证 | 不负责命令内容策略 |

**硬规则：** 即使 Agent 绕过 Skill 直接调 CLI，策略仍必须生效。

---

## 3. 二进制与全局约定

### 3.1 命令名

- 二进制名：**`ssh-remote`**（可在实现时用 build 标签改名，但文档与 Skill 以此为准）

### 3.2 全局行为

| 项 | 合同 |
| --- | --- |
| stdout | **仅**输出一个 JSON 对象（UTF-8）；成功或失败皆然 |
| stderr | 可选人类调试信息；**Agent 不得依赖 stderr 作为合同** |
| 非交互 | 调用 `ssh`/`scp` 必须带非交互友好选项（见 §7） |
| 配置路径 | 策略文件默认：`$XDG_CONFIG_HOME/ssh-remote/policy.toml`；若未设 XDG，则为 `~/.config/ssh-remote/policy.toml` |
| Host 配置 | 仅使用用户 OpenSSH 配置（默认 `~/.ssh/config`）；可用全局 flag 覆盖路径 |

### 3.3 全局 flags（所有子命令可用）

| Flag | 默认 | 说明 |
| --- | --- | --- |
| `--config <path>` | 系统默认 ssh config | 传给 ssh/scp 的 `-F` |
| `--policy <path>` | 见 §3.2 | 策略文件路径 |
| `--timeout <duration>` | policy / 默认 60s | 覆盖本次超时（如 `30s`、`2m`） |
| `--workdir <path>` | 空 | 仅 `exec`：在远端先 `cd` 再执行（实现须安全 shell 引用） |

---

## 4. 子命令合同

### 4.1 `ssh-remote hosts`

**用途：** 列出可从 ssh config 解析出的 Host 别名（供 Agent 发现目标）。

**参数：** 无位置参数。

**行为：**

- 解析 `--config` 指向的文件（及 OpenSSH 常规 include 行为：S2 **最低要求**能解析主文件中的 `Host` 行；`Include` 递归为 should）
- **排除**仅含模式字符的 Host（`*`、`?`），避免把 `Host *` 当成可连目标
- 不探测网络、不尝试连接

**成功 JSON：** 见 §5.2 `hosts`

---

### 4.2 `ssh-remote exec <host> -- <command…>`

**用途：** 在 `<host>` 上执行命令。

**参数：**

| 参数 | 说明 |
| --- | --- |
| `<host>` | Host 别名或 ssh 可接受的目标名（与日常 `ssh host` 一致） |
| `--` 后 | 远端命令 argv；**推荐** Agent 使用 `--` 防止 flag 吞并 |
| 无 `--` | 允许：`exec <host> <command…>`，但第一个非 flag 后全部视为命令 |

**策略顺序：**

1. 将命令规范为单一字符串（用于匹配）：以空格 join argv（实现须在 JSON 的 `command` 字段回显实际发送内容）  
2. 命中 **命令黑名单** → 不调用 ssh，返回策略拒绝（exit 2）  
3. 调用系统 ssh 执行  
4. 对合并收集的 stdout/stderr 做字节上限截断  
5. 组装 JSON；按 §6 设 exit code  

**远端工作目录：** 若 `--workdir` 非空，实际远端命令等价于：

```text
cd -- <workdir> && <user-command>
```

（`workdir` 与 command 的引用规则由实现保证，防止注入；失败时 error 类为 `local` 或 `remote` 需可区分。）

---

### 4.3 `ssh-remote get <host> <remote-path> [local-path]`

**用途：** 下载远程文件到本地。

**参数：**

| 参数 | 说明 |
| --- | --- |
| `<remote-path>` | 远端文件路径（S2：仅普通文件；目录 out of scope） |
| `[local-path]` | 可选；默认写到当前目录下远端 basename |

**策略：**

1. 传输前若能廉价得知大小且超过 `maxFileBytes` → 拒绝（exit 2）  
2. 使用 `scp` 或 `sftp`（实现任选，须可测）  
3. 若传输后本地文件超过 `maxFileBytes` → 删除不完整产物（best effort），返回策略/限额错误  
4. 不把文件内容写入 JSON（只返回路径与字节数）  

**S2 不做：** 递归目录、通配多文件、远端管道。

---

### 4.4 `ssh-remote put <host> <local-path> <remote-path>`

**用途：** 上传本地文件到远端 **写路径白名单** 内。

**策略顺序：**

1. 本地路径必须是普通文件且存在  
2. 本地大小 > `maxFileBytes` → 拒绝（exit 2）  
3. 将 `<remote-path>` 规范为绝对路径规则（见 §8.2）后做白名单匹配；不通过 → exit 2  
4. 传输；失败按连接/远端错误返回  

**模式 α：** 上传成功后 **不** 禁止后续 `exec`；是否执行由 Agent/人类决定，仅受命令黑名单约束。

---

## 5. JSON 响应合同

### 5.1 公共字段

每次调用 stdout **恰好一个** JSON object：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `ok` | boolean | 是 | 是否达成子命令业务成功 |
| `action` | string | 是 | `hosts` \| `exec` \| `get` \| `put` |
| `error` | object \| null | 是 | 失败时非 null；成功时 `null` |
| `error.code` | string | 失败时 | 机器可读码，见 §5.4 |
| `error.message` | string | 失败时 | 简短人类可读说明 |
| `meta` | object | 是 | 运行元数据 |
| `meta.version` | string | 是 | CLI 版本 |
| `meta.host` | string \| null | 是 | 涉及的 host；`hosts` 时为 null |
| `meta.timeout_ms` | number | 是 | 本次生效超时 |
| `meta.truncated` | boolean | 条件 | 输出或内容被截断时出现/为 true |
| `result` | object \| null | 是 | 成功时业务体；失败时可为 null 或部分信息 |

### 5.2 `result` 按 action

**`hosts`**

```json
{
  "ok": true,
  "action": "hosts",
  "error": null,
  "meta": { "version": "0.1.0", "host": null, "timeout_ms": 60000 },
  "result": {
    "hosts": ["prod-api", "bastion", "dev-box"],
    "config_path": "/Users/x/.ssh/config"
  }
}
```

**`exec`**

```json
{
  "ok": true,
  "action": "exec",
  "error": null,
  "meta": {
    "version": "0.1.0",
    "host": "prod-api",
    "timeout_ms": 60000,
    "truncated": false
  },
  "result": {
    "command": "df -h",
    "exit_code": 0,
    "stdout": "...",
    "stderr": "",
    "workdir": null
  }
}
```

说明：

- `ok: true` 当且仅当：策略通过、ssh 连接成功、**且**远端 `exit_code == 0`  
- 远端非 0：`ok: false`，`error.code = "remote_exit"`，`result` 仍应带上 stdout/stderr/exit_code（便于排障）  
- `stdout`/`stderr` 为截断后的字符串；超限时 `meta.truncated: true`  

**`get` / `put`**

```json
{
  "ok": true,
  "action": "get",
  "error": null,
  "meta": { "version": "0.1.0", "host": "prod-api", "timeout_ms": 60000 },
  "result": {
    "remote_path": "/var/log/nginx/error.log",
    "local_path": "/tmp/error.log",
    "bytes": 1204
  }
}
```

### 5.3 策略拒绝示例

```json
{
  "ok": false,
  "action": "put",
  "error": {
    "code": "policy_denied",
    "message": "remote path not in write allowlist: /etc/nginx/nginx.conf"
  },
  "meta": { "version": "0.1.0", "host": "prod-api", "timeout_ms": 60000 },
  "result": null
}
```

### 5.4 `error.code` 枚举（S2）

| code | 含义 |
| --- | --- |
| `usage` | 参数/用法错误 |
| `policy_denied` | 黑名单、白名单、文件大小等策略 |
| `remote_exit` | 远端命令退出码非 0 |
| `connect` | 认证失败、主机不可达、ssh 握手失败等 |
| `timeout` | 超过超时 |
| `local_fs` | 本地文件不存在、非普通文件、写本地失败等 |
| `internal` | 未归类内部错误 |

---

## 6. 进程退出码

| Exit | 含义 | 典型 `error.code` |
| --- | --- | --- |
| `0` | 业务成功（`ok: true`） | — |
| `1` | 本地用法/本地文件系统错误 | `usage`, `local_fs` |
| `2` | 策略拒绝 | `policy_denied` |
| `3` | 远端命令非 0 | `remote_exit` |
| `4` | 连接失败或超时 | `connect`, `timeout` |
| `5` | 内部错误 | `internal` |

**合同：** Agent 应 **同时** 看 exit code 与 JSON；以 JSON 为详情源。

---

## 7. 系统 OpenSSH 调用约定

### 7.1 `exec`

最低选项意图（具体 flag 以实现为准，须满足语义）：

| 语义 | 说明 |
| --- | --- |
| 非交互 | 无法弹密码提示时应快速失败（如 `BatchMode=yes`） |
| 禁用伪终端 | 避免 TTY 分配导致挂起（如 `-T`） |
| 保留 config | 默认读取用户 ssh config；`--config` → `-F` |
| 远程命令 | `ssh [opts] host -- remote-command` 形式，避免本机 shell 二次展开意外 |

### 7.2 `get` / `put`

- 使用本机 `scp` 或 `sftp` 子系统，**同样**尊重 ssh config / ProxyJump  
- 不在 argv 中打印或回显密钥材料  

### 7.3 超时

- 对子进程施加整体超时（`meta.timeout_ms`）  
- 超时后杀进程树（best effort），`error.code = timeout`，exit 4  

---

## 8. 策略文件（policy.toml）

### 8.1 路径与合并

1. 加载内置默认（代码内）  
2. 若 `--policy` 或默认路径文件存在，**覆盖式合并**（文件中出现的键覆盖默认）  
3. 未知键：S2 建议 **警告到 stderr 并忽略** 或 **直接失败**——实现选一种并在 README 写死；推荐 **失败启动/调用**（与 ssh-mcp 严格配置同思路，防拼写失效）  

### 8.2 默认值（T6）

```toml
# 语义示意；字段名以实现 schema 为准，但数值与含义冻结如下

command_timeout_ms = 60000
max_output_bytes = 1048576          # 1 MiB，stdout+stderr 各自或合计：S2 定为「stdout 与 stderr 分别上限各 max_output_bytes」
max_file_bytes = 5242880            # 5 MiB

# 命令黑名单：正则，对「规范化后的命令字符串」匹配任一即拒绝
# 内置默认须包含（可追加，不可在默认中缺席）高危模式，例如：
# rm -rf /、mkfs、dd of=/dev、:(){ :|:& };:、磁盘设备写等
command_denylist = [
  # 完整列表实现时写入 defaults；Spec 要求「拒绝明显毁盘/叉弹」而非穷尽 shell 技巧
]

# 写路径白名单：put 的远端路径必须匹配至少一条
# 支持前缀与 ** 语义（实现可用简单规则）：
# - "/tmp/" 前缀
# - "~/agent-drop/" 表示远端 home 下 agent-drop（见下）
write_allowlist = [
  "/tmp/",
  "~/agent-drop/",
]
```

**路径匹配规则（冻结）：**

1. 拒绝空路径、拒绝 `..` 段（规范化后仍含 `..` 则 `policy_denied`）  
2. `~/` 前缀：匹配「远端 home 相对路径」；S2 实现可在 put 前 `ssh host 'printf %s "$HOME"'` 展开，或要求用户写绝对路径 + 仅允许 `/tmp/`——**推荐展开 $HOME** 一次并缓存于单次 CLI 进程  
3. 白名单条目若以 `/` 结尾，表示目录前缀；否则表示精确文件路径或前缀（S2 简化：**全部按前缀匹配**，条目统一写成带尾 `/` 的目录或明确文件全路径）  
4. 不匹配 → `policy_denied`  

### 8.3 命令黑名单规则（冻结）

1. 匹配目标：将要发送到远端的命令字符串（含 workdir 包装后的最终串，或包装前用户串——**S2 定为对用户原始 command 与最终发送串都匹配，任一命中即拒**）  
2. 正则使用 RE2/Go `regexp` 语义  
3. 黑名单 **不能** 被 CLI flag 关闭（无 `--insecure`）；只能改 policy 文件放宽  
4. 默认列表以「防误操作为主」，不宣称防御专业恶意绕过  

---

## 9. 运维 Skill 合同（行为级，非全文）

Skill 文档（实现阶段另写 `SKILL.md`）必须包含：

| 条款 | 要求 |
| --- | --- |
| 触发 | 用户要查远程日志、进程、磁盘、服务状态、拉/放小文件等 |
| 前置 | 优先 `hosts` 或用户已给 Host 别名；不猜测密码 |
| 调用 | 只通过 `ssh-remote` 子命令；解析 stdout JSON |
| 安全 | 不教 Agent 用裸 `ssh` 绕过；不把密钥写入对话 |
| 有限写 | put 仅白名单；说明模式 α：上传后可 exec，但仍受黑名单约束 |
| 大输出 | 尊重 `truncated`；需要更多时改用 `get` 拉文件或缩小命令范围 |
| 失败 | 按 `error.code` 分支：策略拒绝要向用户解释并改命令/路径，而非重试暴力绕过 |

Skill **不** 实现策略引擎；只引用本 Spec 的行为。

---

## 10. 验收标准（S2 Done）

| # | 标准 |
| --- | --- |
| A1 | `hosts` 能列出当前 `~/.ssh/config` 中非通配 Host 别名 |
| A2 | `exec` 对允许命令返回 JSON，含 stdout/stderr/exit_code |
| A3 | 黑名单命令不发起危险操作（至少不调用成功执行该命令的 ssh 业务路径），exit 2 + `policy_denied` |
| A4 | `get` 下载小文件，JSON 含 local/remote/bytes |
| A5 | `put` 到 `/tmp/` 下成功；`put` 到 `/etc/...` 被拒绝 |
| A6 | 超大输出截断且 `meta.truncated: true` |
| A7 | 超时返回 `timeout` 与 exit 4 |
| A8 | 无 policy 文件时内置默认仍安全可用 |
| A9 | 存在一份可安装的运维 Skill 草稿，指示 Agent 仅通过 CLI 操作 |
| A10 | 单测或集成测覆盖：策略拒绝、exec 成功、JSON 形状 |

---

## 11. 二期（明确延期）

- NPM 多平台二进制包装  
- `Include` 完整 ssh config 语义强化  
- 目录递归传输、远程编辑、备份回滚  
- 审批流 / 审计日志文件  
- per-host 策略覆盖  

交互 Session / 后台 `tail -f` **不是** S2 的第二种 mode。日常排障维持 CLI + 连接复用；若未来单独立项，须另开 ADR（见 ADR-0003 与 `docs/notes/connection-reuse-vs-session.md`）。

---

## 12. 开放小项（不挡 S2 开工）

| 项 | 默认倾向 |
| --- | --- |
| scp vs sftp | 实现选更简单且尊重 ProxyJump 的一条 |
| `max_output_bytes` 对 stdout/stderr 合计还是分别 | **分别**各上限（已写在 §8.2） |
| 未知 policy 键 | **调用失败**（推荐） |
| 命令名是否加 `sr` 短别名 | S2 只保证 `ssh-remote` |

---

## 13. 文档与代码落点（实现时）

```text
ssh-remote/                 # 建议新建 Go 模块根（与参考 MCP 仓库并列或替换工作区布局）
  cmd/ssh-remote/
  internal/policy/
  internal/sshwrap/
  internal/outjson/
  docs/ 或沿用本文件
  skill/ 或 ~/.pi/agent/skills/ssh-remote/
```

本 Spec 路径：`docs/spec/s2-cli-skill.md`。  
领域词：`CONTEXT.md`。决策：`docs/adr/0001-*.md`、`0002-*.md`、`0003-*.md`。连接复用说明：`docs/notes/connection-reuse-vs-session.md`。
