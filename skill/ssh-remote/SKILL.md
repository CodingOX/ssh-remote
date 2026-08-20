---
name: ssh-remote
description: >
  通过本机 ssh-remote CLI 对远程主机进行只读排障，以及受策略约束的小型文件传输和已审脚本执行。
  当用户要 SSH 连接服务器、诊断 SSH 配置或 Host 别名、查看远程日志/进程/磁盘/服务状态、传输小型配置或脚本，
  或在远端安装经校验的官方制品时使用。
---

# ssh-remote 运维 Skill

使用 `ssh-remote` 操作远程主机。CLI 会强制命令黑名单、文件路径白名单、文件大小和超时限制；先用其结果定位问题，再决定下一步。

## 开始前

1. 执行 `ssh-remote version` 确认命令可用。
2. 执行 `ssh-remote hosts`，仅使用 `result.hosts` 中的 Host 别名和连接字段确认目标，不猜测账号、地址或跳板。
3. 命令缺失时，读取 [references/installation.md](references/installation.md)。
4. Host 缺失、连接失败或需要调整策略时，读取 [references/configuration.md](references/configuration.md)。

目标机应已由用户配置在 `~/.ssh/config` 中；认证、跳板和主机密钥校验由系统 OpenSSH 处理。

## 日常工作流

```text
确认目标：hosts
    |
    +-- Host 或连通性异常：doctor <host>
    |
    +-- 正常：exec 做只读探查
                  |
                  +-- 需要小文件：get / put
                  |
                  +-- 需要脚本：本机审阅 -> put -> exec
                  |
                  +-- 需要官方大制品：远端下载 -> 校验哈希/签名 -> 安装
```

优先从只读命令开始，例如 `df -h`、`uptime`、`systemctl status <service>`、`tail -n 100 <log>` 和 `last reboot`。

每次调用只解析 stdout 的 JSON，不依赖 stderr。通用结构是 `ok`、`action`、`result`、`error`、`retriable` 与 `meta.truncated`。需要完整字段或自动化解析时，读取 [references/json-contract.md](references/json-contract.md)。

## 子命令

| 命令 | 用途 |
| --- | --- |
| `ssh-remote version` | 输出版本；不加载 policy |
| `ssh-remote hosts` | 列出 SSH Host 与解析后的连接字段 |
| `ssh-remote doctor <host>` | 检查本地前置、Host 解析、策略与可选连通探测 |
| `ssh-remote policy` | 输出当前生效策略快照 |
| `ssh-remote exec <host> -- <cmd...>` | 远程执行命令 |
| `ssh-remote get <host> <remote> [local]` | 下载小文件 |
| `ssh-remote put <host> <local> <remote>` | 上传到允许的远端路径 |

可按需使用 `--config`、`--policy`、`--timeout 30s`；`--workdir /path` 仅适用于 `exec`。

## 错误分流

- `connect`：SSH 握手、认证、跳板或主机可达性异常。先运行 `doctor <host>`，再检查 SSH 配置。
- `timeout`：连接已建立但命令或传输超时。缩小命令范围、拆分任务或提高 `--timeout`。
- `remote_fs`：连接已建立，远端路径、权限或文件类型不正确。确认路径和权限，不要改 SSH 配置。
- `policy_denied`：策略拒绝。解释 `error.message`，改用允许的命令或路径；用户需要调整策略时再读取配置参考。
- `remote_exit`：远端命令返回非零。继续分析 `result` 中的 stdout、stderr 与 exit_code。
- `meta.truncated=true`：缩小输出范围，或用 `get` 拉取允许读取的小文件。

## 文件与安全边界

- 不回显私钥、密码或其他秘密材料；远端操作始终使用 `ssh-remote`，不以裸 `ssh`、`scp` 或 MCP 绕开 CLI 策略。
- `get`、`put` 仅用于小文件；敏感路径与默认限制见 [references/safety-and-transfer.md](references/safety-and-transfer.md)。
- 执行脚本前，先在本机读取并审阅；再上传到允许路径并执行。
- 不执行“远端下载未知脚本后立即运行”的流程。官方制品在远端下载后，必须先验证官方哈希或签名。
- 不重复提交同一个被 `policy_denied` 拒绝的命令或路径。

## 常用示例

```bash
ssh-remote version
ssh-remote hosts
ssh-remote doctor prod-api
ssh-remote exec prod-api -- df -h
ssh-remote exec prod-api --workdir /var/log/nginx -- tail -n 50 error.log
ssh-remote get prod-api /etc/nginx/nginx.conf /tmp/nginx.conf
ssh-remote put prod-api ./fix.sh /tmp/fix.sh
ssh-remote exec prod-api -- bash /tmp/fix.sh
```

## 参考资料

- 命令未安装、需校验 Release 或从源码构建：[references/installation.md](references/installation.md)
- Host、`policy.toml`、路径白名单与只读模式：[references/configuration.md](references/configuration.md)
- JSON 字段、动作结果和退出码：[references/json-contract.md](references/json-contract.md)
- 敏感路径、脚本和官方制品传输规则：[references/safety-and-transfer.md](references/safety-and-transfer.md)
