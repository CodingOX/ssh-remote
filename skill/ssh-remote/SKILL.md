---
name: ssh-remote
description: >
  通过本机 ssh-remote CLI（Go 薄封装系统 OpenSSH）对远程主机排障与有限文件操作。
  当用户要 SSH 连服务器、看远程日志/进程/磁盘/服务状态、下载配置或日志、上传脚本到允许路径、
  或提到 Host 别名 / ~/.ssh/config 远程操作时使用。不要用裸 ssh 绕过本工具；不要使用 MCP。
---

# ssh-remote 运维 Skill

用 **`ssh-remote` CLI** 操作远程主机。策略（命令黑名单、写路径白名单、大小/超时）在 CLI 内强制，本 Skill 不能也不应绕过。

## 前置

1. PATH 中可执行 `ssh-remote`（开发期可用 `go run ./cmd/ssh-remote`）。
2. 目标机已在 `~/.ssh/config` 配置好（密钥/跳板由系统 ssh 处理）。
3. **只解析 CLI 的 stdout JSON**；不要依赖 stderr。

## 子命令

| 命令 | 用途 |
| --- | --- |
| `ssh-remote hosts` | 列出 Host 别名 |
| `ssh-remote exec <host> -- <cmd…>` | 远程执行 |
| `ssh-remote get <host> <remote> [local]` | 下载小文件 |
| `ssh-remote put <host> <local> <remote>` | 上传到写白名单路径 |

全局可选：`--config`、`--policy`、`--timeout 30s`、`--workdir /path`（仅 exec）。

## JSON 合同（必读）

每次调用 stdout 为一个 JSON：

- `ok`：业务是否成功  
- `error.code`：`usage` \| `policy_denied` \| `remote_exit` \| `connect` \| `timeout` \| `local_fs` \| `internal`  
- `result`：业务字段（exec 含 `stdout`/`stderr`/`exit_code`）  
- `meta.truncated`：输出是否被截断  

退出码：`0` 成功 · `1` 用法/本地 · `2` 策略 · `3` 远端非0 · `4` 连接/超时 · `5` 内部。

远端命令非 0 时：`ok=false` 且 `error.code=remote_exit`，但 `result` 仍可能带 stdout/stderr——**继续用来排障**。

## 安全约定

1. **禁止**教用户或自己改用裸 `ssh`/`scp` 绕过策略。  
2. **禁止**在对话中回显私钥、密码、`SSH_AUTH_SOCK` 以外的秘密材料。  
3. **有限写（模式 α）**：`put` 默认仅允许  
   - `/tmp/` 下  
   - 远端 `~/agent-drop/` 下  
   上传后可以再 `exec`，但仍过命令黑名单。  
4. `put` 到 `/etc`、家目录任意路径、业务数据盘等会被 `policy_denied`——应改路径或请用户改 `~/.config/ssh-remote/policy.toml`。  
5. 高危命令（如 `rm -rf /`、`mkfs`、`dd of=/dev/...`）会被拒绝。  
6. 输出过大时看 `meta.truncated`：改用更窄的命令或 `get` 拉文件，不要死循环重试同一大输出命令。

## 推荐排障流程

```text
1) 确认 Host：ssh-remote hosts  或用户给定别名
2) 只读探查：exec … -- 'df -h' / 'uptime' / 'systemctl status …'
3) 看日志：exec … -- 'tail -n 100 /var/log/…'
   或 get 小日志文件到本地再分析
4) 需要脚本：把脚本 put 到 /tmp/ 或 ~/agent-drop/ 再 exec
5) 策略拒绝：向用户解释 error.message，改命令或路径，不要暴力重试
```

## 示例

```bash
ssh-remote hosts
ssh-remote exec prod-api -- df -h
ssh-remote exec prod-api --workdir /var/log/nginx -- tail -n 50 error.log
ssh-remote get prod-api /etc/nginx/nginx.conf /tmp/nginx.conf
ssh-remote put prod-api ./fix.sh /tmp/fix.sh
ssh-remote exec prod-api -- bash /tmp/fix.sh
```

## 不做

- 不启动 MCP  
- 不维护第二套主机密码库  
- 不打开交互 PTY / 长驻 session  
- 不把文件全文塞进 JSON（get/put 只返回路径与字节数）
