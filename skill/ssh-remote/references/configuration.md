# Host 与策略配置

只在 `hosts` / `doctor` 表明前置异常，或用户明确希望调整 CLI 策略时读取本文件。

## SSH Host

先执行：

```bash
ssh-remote hosts
ssh-remote doctor <host>
```

- 从 `result.hosts` 的 `name`、`hostname`、`user`、`port`、`proxy_jump` 确认目标。
- `doctor` 的 `host_found=false` 表示目标不在 SSH 配置中；引导用户在 `~/.ssh/config` 添加 `Host`、`HostName`、`User`、`IdentityFile`，并按需添加 `ProxyJump`。
- 不猜测用户名、地址、跳板或认证方式，也不要求用户回显密码和私钥。
- `probe.probe_ok=false` 时，优先根据 `probe_error_code` 和 SSH 配置定位问题。

## policy.toml

配置路径为 `~/.config/ssh-remote/policy.toml`。没有该文件时，CLI 使用内置安全默认值。

`write_allowlist`、`read_denylist` 和 `local_denylist` 是覆盖式配置：一旦声明，就必须显式保留仍需使用的默认条目。改动前先运行 `ssh-remote policy` 记录当前快照，改动后再次运行确认生效。

写入路径示例：

```toml
# 可选。声明后会覆盖 CLI 默认的整条白名单。
write_allowlist = [
  "/tmp/",
  "~/agent-drop/",
  # 追加经用户确认的目录；尾 / 表示目录前缀
]
```

可配置键：`command_timeout_ms`、`max_output_bytes`、`max_file_bytes`、`max_command_chars`、`read_only`、`command_allowlist`、`command_denylist`、`write_allowlist`、`read_denylist`、`local_denylist`。

- 目录规则：尾随 `/` 表示目录前缀；`~/` 按远端 home 展开；相对路径和 `..` 被拒绝。
- `read_only=true` 时，`put` 被拒绝，`exec` 只允许 CLI 冻结的只读命令集合且不允许 shell 控制符。
- 非空 `command_allowlist` 会要求命令匹配正则。
- `put` 的父目录只有同时落在写白名单中时，CLI 才会自动创建。
