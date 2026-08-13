---
name: ssh-remote
description: >
  通过本机 ssh-remote CLI（Go 薄封装系统 OpenSSH）对远程主机排障与有限文件操作。
  当用户要 SSH 连服务器、看远程日志/进程/磁盘/服务状态、下载配置或日志、上传脚本到允许路径、
  在远端安装官方包（JDK / Maven / 类似制品）、或提到 Host 别名 / ~/.ssh/config 远程操作时使用。
  不要用裸 ssh 绕过本工具；不要使用 MCP。
---

# ssh-remote 运维 Skill

用 **`ssh-remote` CLI** 操作远程主机。策略（命令黑名单、写路径白名单、大小/超时）在 CLI 内强制，本 Skill 不能也不应绕过。

## 前置

1. PATH 中可执行 `ssh-remote`（开发期可用 `go run ./cmd/ssh-remote`）。
2. 目标机已在 `~/.ssh/config` 配置好（密钥/跳板由系统 ssh 处理）。
3. **只解析 CLI 的 stdout JSON**；不要依赖 stderr。

## 引导（前置未就绪时）

前置不满足时**引导用户补配置**，不替用户改系统配置、不猜测密码。完整步骤见仓库 `docs/quickstart.md`。

**1) Host 不在列表 / connect 失败**
- 先 `ssh-remote hosts`，读 `result.hosts` 里每条对象的 `name` / `hostname` / `user` / `port` / `proxy_jump` 确认目标——**不要猜账号或跳板**
- 仍不确定或 connect 失败 → **`ssh-remote doctor <host>`**（见 §子命令 `doctor`）：查 `host_found`、config 字段、`probe.probe_ok`；**前置失败先 doctor，再改 config/密钥**
- 用户要的 Host 没有 → 引导建立 `~/.ssh/config`：
  `ssh-keygen -t ed25519` → `ssh-copy-id user@host` → 写 Host 块（HostName / User / IdentityFile，可选 ProxyJump）
- 认证与跳板全走系统 OpenSSH；本 Skill 不存、不回显密码密钥
- **`error.code=connect`**：SSH 握手/认证/跳板没通（不可达通常较快失败，CLI 对 ssh/scp 设 `ConnectTimeout=10`）。排查 config、密钥、ProxyJump——**不要**把 connect 当成「命令跑太久」
- **`error.code=timeout`**：已连上但命令或 scp 超过 `--timeout`/policy 时限。**不要**因此去改密钥或重配 SSH；应缩窄命令、拆任务或加大超时
- **`error.code=remote_fs`**：连接已通，但远端路径/权限/文件类型不对（如 No such file、Permission denied on path）。**不要**当 connect 去改密钥；应改路径、权限或先 `exec` 确认文件存在

**2) put 被 policy_denied**
- 先解释 `error.message`，优先建议换白名单内路径（`/tmp/`、`~/agent-drop/`）
- 用户确认要放宽 → 引导建立/修改 `~/.config/ssh-remote/policy.toml`：

```toml
# 复制 config/policy.example.toml 到 ~/.config/ssh-remote/policy.toml
# ⚠️ write_allowlist 是覆盖式：一旦写了就整表替换默认，必须带上默认两条
write_allowlist = [
  "/tmp/",
  "~/agent-drop/",
  # 追加用户目录；尾 / 表示目录前缀，不带 / 表示精确文件
]
```

- 匹配规则：尾 `/` = 目录前缀；`~/` 按远端 home 展开；相对路径与 `..` 一律拒绝
- 改完先 `ssh-remote policy` 确认生效快照，再 `ssh-remote hosts` 验证解析（未知键会直接报错；合法键见 `config/policy.example.toml`），再重试 put

**3) get/put 路径被拒（policy_denied / remote_fs）**
- `get` 默认拒读远端 `~/.ssh/`、`/etc/shadow`、任意 `authorized_keys`；`put` 禁止本机源在 `~/.ssh/`；`get` 禁止写到本机 `~/.ssh/` 或含 `.git` 的路径——均需改路径，**禁止**用裸 scp 绕过
- 用户确认要放宽读/写/本机路径限制 → 在 policy.toml 配置 `read_denylist` / `local_denylist`（均为**覆盖式**，须带上默认条目）；详见 `config/policy.example.toml`

## 子命令

| 命令 | 用途 |
| --- | --- |
| `ssh-remote version` / `--version` / `-V` | 输出 CLI 版本（JSON，`action=version`）；**不加载 policy**，坏 policy.toml 也能查版本 |
| `ssh-remote hosts` | 列出 `~/.ssh/config` 中的 Host 条目（含连接字段） |
| `ssh-remote doctor <host>` | 非破坏性诊断：本机 ssh/scp、config 解析、可选连通 probe |
| `ssh-remote policy` | 输出当前生效策略快照（不回显密钥） |
| `ssh-remote exec <host> -- <cmd…>` | 远程执行 |
| `ssh-remote get <host> <remote> [local]` | 下载小文件 |
| `ssh-remote put <host> <local> <remote>` | 上传到写白名单路径 |

全局可选：`--config`、`--policy`、`--timeout 30s`、`--workdir /path`（仅 exec）。

**连接复用（默认开启）：** CLI 对 ssh/scp 传 `ControlMaster=auto` + `ControlPersist=60s`，同 Host 多次调用复用握手；Agent **用法不变**，无需显式管理 session。

## JSON 合同（必读）

每次调用 stdout 为一个 JSON：

- `ok`：业务是否成功
- `action`：`version` \| `hosts` \| `doctor` \| `policy` \| `exec` \| `get` \| `put`
- `retriable`：是否建议同参重试（见下）
- `error.code`：`usage` \| `policy_denied` \| `remote_exit` \| `connect` \| `timeout` \| `local_fs` \| `remote_fs` \| `internal`
- `result`：按 `action` 变化（见下）
- `meta.truncated`：输出是否被截断

**`retriable` 与 `error.code`（§02）：**

| `error.code` | `retriable` | 含义与 Agent 动作 |
| --- | --- | --- |
| `connect` | `true` | SSH/scp 层握手/认证失败；查 config/密钥/跳板，**可**短暂重试 |
| `timeout` | `true` | 已连上但超时；缩命令/拆任务/加 `--timeout`，**可**重试 |
| `remote_fs` | `false` | 已连上，远端路径/权限/文件类型问题；**改路径**，勿当 connect |
| `policy_denied` | `false` | 策略拒绝；向用户解释 `error.message`，**禁止**同参暴力重试或改 policy 绕过 |
| 其它 | `false` | 按码分支处理 |

**`connect` / `timeout` / `remote_fs` 分开：**
- `connect` — SSH/scp 握手失败（认证、跳板、主机不可达等）
- `timeout` — 连接已建立，但远程命令或传输超过时限
- `remote_fs` — 连接已建立，远端文件系统/权限问题（`scp: <path>: Permission denied`、No such file、mkdir 失败）
- 无路径上下文的 `Permission denied` / `please try again` / `Host key verification failed` 是 **connect**，不是 remote_fs
- `connect` 与 `timeout` exit 码均为 `4`；`remote_fs` 为 `6`——**必须看 `error.code` 区分**；禁止把 `timeout`/`remote_fs` 当 `connect` 去改密钥

退出码：`0` 成功 · `1` 用法/本地 · `2` 策略 · `3` 远端非0 · `4` 连接/超时 · `5` 内部 · `6` 远端 FS。

远端命令非 0 时：`ok=false` 且 `error.code=remote_exit`，但 `result` 仍可能带 stdout/stderr——**继续用来排障**。

### `result` 按 action

**`version`** — `result.version` 为 CLI 语义版本字符串。

**`hosts`** — `result.hosts` 为**对象数组**（空字段保留键、值为 `""`）：

| 字段 | 含义 |
| --- | --- |
| `name` | `~/.ssh/config` 中的 Host 别名（exec/get/put 用这个） |
| `hostname` | HostName |
| `user` | User |
| `port` | Port |
| `proxy_jump` | ProxyJump |

另有 `result.config_path`。用这些字段确认目标与跳板，**不探测网络、不猜账号**。

**`doctor`** — 诊断对象（空字段保留键）：

| 字段 | 含义 |
| --- | --- |
| `ssh_binary` / `scp_binary` | 本机是否找到可执行文件 |
| `host_found` | Host 是否在 ssh config 中 |
| `hostname` / `user` / `port` / `proxy_jump` | 解析出的连接字段 |
| `policy_ok` | 策略是否已加载 |
| `mux_enabled` | **本次实际**是否带上 ControlMaster（缓存目录不可写时为 false，不是配置意图） |
| `probe` | 可选；含 `probe_ok`、`probe_error_code`、`message` |

**`policy`** — 生效策略快照：`policy_path`、`command_timeout_ms`、`max_output_bytes`、`max_file_bytes`、`max_command_chars`、`read_only`、`command_denylist`、`command_allowlist`、`write_allowlist`、`read_denylist`、`local_denylist`（空切片保留键）。

**`exec`** — `stdout` / `stderr` / `exit_code` / `command` / `workdir`（可选）。

**`get` / `put`** — 路径与字节数等；不把文件全文塞进 JSON。

## 安全约定

1. **禁止**教用户或自己改用裸 `ssh`/`scp` 绕过策略；也**禁止**教 Agent 改 policy 黑名单来「绕过」默认拒绝。
2. **禁止**在对话中回显私钥、密码、`SSH_AUTH_SOCK` 以外的秘密材料。
3. **有限写（模式 α）**：`put` 默认仅允许
   - `/tmp/` 下
   - 远端 `~/agent-drop/` 下
   上传后可以再 `exec`，但仍过命令黑名单。
4. **`put` 白名单内自动建父目录（§05）**：目标文件的父目录也须在 `write_allowlist` 内时，CLI 会先 `mkdir -p` 再 scp；父目录不在白名单 → `policy_denied`，不会越权 mkdir。
5. **敏感路径（§04）**：
   - `get` 默认拒读远端 `~/.ssh/`、`/etc/shadow`、任意路径下的 `authorized_keys`
   - `put` 禁止本机源路径在 `~/.ssh/`（`local_denylist`）
   - `get` 禁止写到本机 `~/.ssh/` 或含 `.git` 段的路径
   - 可通过 `read_denylist` / `local_denylist` 收紧或放宽（**覆盖式**，须自行保留需要的默认条目）
6. `put` 到 `/etc`、家目录任意路径、业务数据盘等会被 `policy_denied`——应改路径或请用户改 `~/.config/ssh-remote/policy.toml`。
7. **只读策略（§08）**：`read_only=true` 时拒一切 `put`；`exec` 仅允许冻结的只读命令（如 `df`、`uptime`、`systemctl status` 等），禁止 shell 控制符。另可配 `command_allowlist`（非空则须匹配正则）与 `max_command_chars`。**排障先只读探查**，确认需要写再关 read_only 或放宽白名单。
8. 默认黑名单会拒绝（举例，非穷举）：
   - 远程拉取并执行：`curl … \| sh`、`wget … \| sh`（**句法保险丝**，拦不住等价变体；变体仍禁止，见下节）
   - 篡改 SSH 授权：`>> ~/.ssh/authorized_keys` 等重定向写 authorized_keys
   - 清空防火墙：`iptables -F`
   - 直接调用：`reboot`、`shutdown`、`halt`、`poweroff`（须作为 shell 命令词出现）
   - 以及 `rm -rf /`、`mkfs`、`dd of=/dev/...` 等其它高危模式
9. **允许**只读查重启信息：`last reboot`、`grep reboot /var/log/…` 等——不会被「reboot 词」误杀。
10. 输出过大时看 `meta.truncated`：改用更窄的命令或 `get` 拉文件，不要死循环重试同一大输出命令。
11. **`policy_denied` 禁止暴力重试**：同命令/同路径反复调用不会通过；须改命令、改路径或请用户改 policy。
12. **脚本 vs 制品**（见下节）：脚本必须本机确认后再 `put` 再 `exec`；大文件制品禁止硬 `put`，远端下载后须校验官方哈希/签名。

## 脚本与制品（下载 / 传输）

把要送到远端的东西分成两类，**不要走同一条路**：

| 类型 | 特征 | 通道 |
| --- | --- | --- |
| **脚本** | 小、人能审（本机写的 helper、安装/修复 shell） | 本机确认 → `put` → `exec` |
| **制品** | 大、人不能通读（JDK / Maven tarball、deb、官方包） | 远端 `curl`/`wget -O` → 校验哈希/签名 → 再安装 |

**脚本（必须）：**

1. 本机编写或下载，**先读过、确认没问题**，禁止盲传。
2. `put` 到 `/tmp/` 或 `~/agent-drop/`（受 `max_file_bytes`，默认 5MiB）。
3. 再 `exec`；上传后的命令仍过黑名单。

**禁止「远程拉取并执行」及其变体。** CLI 默认只拦 `curl … \| sh` 与 `wget … \| sh`；下列写法 CLI **可能放行**，本 Skill **仍禁止**（视为绕过，不要用、不要教）：

- `curl -o x.sh && sh x.sh`、`wget -O x.sh && bash x.sh`（远端先落地再执行未审脚本）
- `curl … \| bash`、`curl … \| python` 等把管道换成别的解释器
- 任何「远端下载一段未知脚本并马上执行」的等价流程

`curl` / `wget` **本身允许**（只下载、不执行）。

**制品（必须）：**

1. **不要走 `put`。** 默认 5MiB 是给小脚本/小配置的；JDK 等会远超，硬传会被 `policy_denied`，也不该把 SSH 当网盘。
2. 在远端 `curl`/`wget -O` 落到 `/tmp`（或其它已允许的可写路径）。
3. **用官方 sha256/sha512 或 GPG 校验通过后**，再 `apt` / `tar` / `dpkg`。信任锚是校验和/签名，不是把上百 MB 读一遍。
4. 需要传「用户自己的、大于 5MiB 的文件」时，才引导用户改 `policy.toml` 的 `max_file_bytes`；**不要**为了装官方包去抬 `put` 上限，也**不要**为此放行 `curl \| sh`。

## 推荐排障流程

```text
1) 确认 Host：ssh-remote hosts → 用 result.hosts[].name/hostname/user/proxy_jump 对齐用户意图
2) 前置异常先诊断：ssh-remote doctor <host> → 看 host_found / probe / mux_enabled
3) 只读探查：exec … -- 'df -h' / 'uptime' / 'systemctl status …'（read_only 模式下仍可用冻结只读集）
4) 看日志：exec … -- 'tail -n 100 /var/log/…' 或 'last reboot'
   或 get 小日志文件到本地再分析（勿 get ~/.ssh、shadow、authorized_keys）
5) 需要脚本：本机确认内容 → put 到 /tmp/ 或 ~/agent-drop/ → exec（禁止远端下载即执行）
   需要官方包/大文件：远端 curl/wget -O → 校验官方哈希或签名 → 再安装（不要 put）
6) connect → 查 SSH/config/密钥/跳板；timeout → 缩命令或加 --timeout；remote_fs → 改路径/权限
7) 策略拒绝：向用户解释 error.message，改命令或路径，不要暴力重试；需要时 ssh-remote policy 核对生效配置
```

## 示例

```bash
ssh-remote --version
ssh-remote hosts
ssh-remote doctor prod-api
ssh-remote policy
ssh-remote exec prod-api -- df -h
ssh-remote exec prod-api --workdir /var/log/nginx -- tail -n 50 error.log
ssh-remote exec prod-api -- last reboot
ssh-remote get prod-api /etc/nginx/nginx.conf /tmp/nginx.conf
# 脚本：本机已确认的 fix.sh → put → exec
ssh-remote put prod-api ./fix.sh /tmp/subdir/fix.sh
ssh-remote exec prod-api -- bash /tmp/subdir/fix.sh
# 制品：远端下载后校验官方哈希（URL 以当时官方为准；不要 put 大包）
ssh-remote exec prod-api -- wget -O /tmp/pkg.tgz "$URL"
ssh-remote exec prod-api -- wget -O /tmp/pkg.tgz.sha512 "$SHA_URL"
ssh-remote exec prod-api -- sha512sum -c /tmp/pkg.tgz.sha512
```

`hosts` 成功时 stdout 形态示意：

```json
{
  "ok": true,
  "action": "hosts",
  "result": {
    "hosts": [
      {
        "name": "prod-api",
        "hostname": "1.2.3.4",
        "user": "ubuntu",
        "port": "",
        "proxy_jump": "bastion"
      }
    ],
    "config_path": "/Users/me/.ssh/config"
  }
}
```

## 不做

- 不启动 MCP
- 不维护第二套主机密码库
- 不打开交互 PTY / 长驻 session
- 不把文件全文塞进 JSON（get/put 只返回路径与字节数）
- 不主动探测网络（ping、扫端口等）；Host 信息只来自 `hosts` 与 `~/.ssh/config`
- 不在远端对未审脚本做「先下载再执行」来绕开 `curl \| sh` 黑名单
- 不把超过 `max_file_bytes` 的官方制品硬 `put`；大文件走远端下载 + 校验
