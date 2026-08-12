# ssh-remote

本机 AI Agent 用的 **Skill + Go 薄 CLI**：封装系统 OpenSSH，对 `~/.ssh/config` 中的 Host 做排障与有限文件操作。

- **不是 MCP**（对照开源方案：[`ssh-mcp`](https://github.com/tufantunc/ssh-mcp)、[`ssh-mcp-server`](https://github.com/classfang/ssh-mcp-server)）
- 合同见 [`docs/spec/s2-cli-skill.md`](docs/spec/s2-cli-skill.md)
- 决策见 [`docs/adr/`](docs/adr/)
- 领域词见 [`CONTEXT.md`](CONTEXT.md)

## 特性

- 复用系统 `ssh` / `scp` 与 `~/.ssh/config`（含跳板、密钥、端口）
- 子命令：`hosts` · `exec` · `get` · `put`
- stdout **始终为单个 JSON**，便于 Agent 解析
- CLI 层强制：命令黑名单、写路径白名单、超时与大小上限

## 安装

```bash
go install github.com/CodingOX/ssh-remote/cmd/ssh-remote@latest
```

或本地构建：

```bash
git clone https://github.com/CodingOX/ssh-remote.git
cd ssh-remote
go build -o bin/ssh-remote ./cmd/ssh-remote
```

需要本机已安装 OpenSSH 客户端（`ssh`、`scp`）。

> 📖 从零到可用的完整引导（SSH 建立、policy.toml、首次验证清单）见 [`docs/quickstart.md`](docs/quickstart.md)。
## 用法

```bash
ssh-remote hosts
ssh-remote exec <host> -- <command...>
ssh-remote get  <host> <remote-path> [local-path]
ssh-remote put  <host> <local-path> <remote-path>
```

全局 flag：`--config`、`--policy`、`--timeout`、`--workdir`。

策略默认：

| 项 | 默认 |
| --- | --- |
| 超时 | 60s |
| 输出上限 | stdout/stderr 各 1MiB |
| 文件上限 | 5MiB |
| put 白名单 | `/tmp/`、`~/agent-drop/` |

可选配置：`~/.config/ssh-remote/policy.toml`（示例见 `config/policy.example.toml`；建立与匹配规则见 [`docs/quickstart.md`](docs/quickstart.md) §3）。

## Skill

草稿：[`skill/ssh-remote/SKILL.md`](skill/ssh-remote/SKILL.md)

安装到 pi：复制到 `~/.pi/agent/skills/ssh-remote/`（或项目 skills 目录）。

## 项目结构

```text
cmd/ssh-remote/     CLI 入口与 flag 解析
internal/
  app/              hosts/exec/get/put 业务
  policy/           策略加载与强制
  runner/           调用系统 ssh/scp
  response/         JSON 信封与退出码
  sshconfig/        解析 Host 别名
  version/          版本号
config/             policy 示例
docs/               Spec 与 ADR
skill/              Agent Skill 草稿
```

## 测试

```bash
go test ./...
```

## 二期

NPM 多平台二进制包装、审计、交互 session 等见 Spec §11。

## License

MIT
