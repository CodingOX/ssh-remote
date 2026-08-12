# 快速上手（Quickstart）

从零到能用的完整引导：**安装 CLI → 建立 SSH → 建立 policy.toml → 首次验证**。
Skill 的前置条件都在这里落地。

## 1. 安装 CLI

```bash
# 方式 A：远程安装
go install github.com/CodingOX/ssh-remote/cmd/ssh-remote@latest

# 方式 B：本地构建
git clone https://github.com/CodingOX/ssh-remote.git
cd ssh-remote
go build -o bin/ssh-remote ./cmd/ssh-remote
```

前置：本机已装 OpenSSH 客户端（`ssh`、`scp`）。

## 2. 建立 SSH（一次性）

CLI 不自建连接，完全复用系统 OpenSSH 与 `~/.ssh/config`。

### 2.1 生成密钥（若还没有）

```bash
ssh-keygen -t ed25519 -C "your-name@machine"
# 一路回车即可，默认生成 ~/.ssh/id_ed25519(.pub)
```

### 2.2 把公钥放上目标机

```bash
ssh-copy-id user@目标机地址
# 或手动把本机 ~/.ssh/id_ed25519.pub 内容追加到目标机 ~/.ssh/authorized_keys
```

### 2.3 写 ~/.ssh/config

给目标机起一个 Host 别名（CLI 只认这个别名）：

```text
Host prod-api
    HostName 1.2.3.4
    User ubuntu
    IdentityFile ~/.ssh/id_ed25519
    # 需要跳板机时取消注释：
    # ProxyJump bastion
```

- 认证、端口、跳板全部交给系统 ssh，CLI 不存任何密码/密钥
- 多个目标机就写多个 `Host` 块

### 2.4 验证 SSH

```bash
ssh-remote hosts                       # 应能看到 prod-api
ssh-remote exec prod-api -- uptime     # 应返回 ok:true
```

## 3. 建立 policy.toml（可选，默认已安全）

CLI 内置安全默认（超时 60s、put 白名单 `/tmp/` 与 `~/agent-drop/` 等），
**没有 policy 文件也能用**。只有需要放宽/收紧时才建立：

### 3.1 复制示例

```bash
cp config/policy.example.toml ~/.config/ssh-remote/policy.toml
# 或尊重 XDG：$XDG_CONFIG_HOME/ssh-remote/policy.toml
```

### 3.2 按需修改

可配的键只有 5 个：`command_timeout_ms`、`max_output_bytes`、`max_file_bytes`、
`command_denylist`、`write_allowlist`。没写的键保留内置默认；**写错键名会直接报错**（防拼写静默失效）。

> ⚠️ **覆盖式语义（最重要）**：`write_allowlist` 与 `command_denylist` 一旦在文件里出现，
> 就**整表替换**内置默认。要放宽白名单，必须把默认条目一起带上：

```toml
write_allowlist = [
  "/tmp/",            # 内置默认，保留
  "~/agent-drop/",    # 内置默认，保留
  "/opt/",            # 自定义追加
  "~/ssh-remote/",    # 自定义追加
]
```

### 3.3 白名单匹配规则

| 写法 | 含义 |
| :--- | :--- |
| `/opt/` | 以 `/` 结尾 = **目录前缀**，`/opt/` 下任意路径都放行 |
| `/etc/hosts` | 不以 `/` 结尾 = **精确文件路径** |
| `~/ssh-remote/` | `~/` 按远端 `$HOME` 展开后再匹配 |
| 相对路径、含 `..` | 一律拒绝 |

### 3.4 验证

```bash
ssh-remote hosts                                # 文件解析失败会立刻报错
ssh-remote put prod-api ./fix.sh /opt/fix.sh    # 白名单内 → 成功
ssh-remote put prod-api ./x.sh /etc/x.sh        # 白名单外 → policy_denied（exit 2）
```

## 4. 首次验证清单

按顺序跑一遍，全部通过即就绪：

| # | 命令 | 预期 |
| :--- | :--- | :--- |
| 1 | `ssh-remote hosts` | 列出你的 Host 别名，`ok:true` |
| 2 | `ssh-remote exec prod-api -- uptime` | `ok:true`，result 带 stdout |
| 3 | `ssh-remote put prod-api ./test.sh /tmp/test.sh` | `ok:true` |
| 4 | `ssh-remote put prod-api ./test.sh /etc/test.sh` | `ok:false`，`policy_denied`，exit 2 |
| 5 | `ssh-remote exec prod-api -- 'rm -rf /'` | 被黑名单拒绝，exit 2 |

## 5. 常见问题

**Q：报 `unknown policy key: xxx`？**
policy.toml 里写了不存在的键。对照 §3.2 的 5 个键名修正。

**Q：put 报 `remote path not in write allowlist`？**
目标路径不在白名单。要么换到白名单路径（如 `/tmp/`），要么按 §3 改 policy.toml 后重试。

**Q：exec 报 `connect`？**
ssh 层没通。用裸 `ssh prod-api` 手动排查（config 别名、密钥、跳板），CLI 只负责转发。

**Q：put 到 `~/xxx/` 报无法展开 `~`？**
CLI 需要先知道远端 home。检查目标机连接是否正常；仍不行就把白名单和路径都写成绝对路径。
