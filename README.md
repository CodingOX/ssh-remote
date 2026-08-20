# ssh-remote

本机 AI Agent 用的 **Skill + Go 薄 CLI**：封装系统 OpenSSH，对 `~/.ssh/config` 中的 Host 做排障与有限文件操作。仅支持 macOS 与 Linux；**不支持 Windows**。WSL 内的 Linux 环境按 Linux 使用。

- **不是 MCP**（对照开源方案：[`ssh-mcp`](https://github.com/tufantunc/ssh-mcp)、[`ssh-mcp-server`](https://github.com/classfang/ssh-mcp-server)）
- 合同见 [`docs/spec/s2-cli-skill.md`](docs/spec/s2-cli-skill.md)
- 决策见 [`docs/adr/`](docs/adr/)（含 [ADR-0003](docs/adr/0003-no-session-reuse-daily-ops.md)：日常排障不做 Session）
- 连接复用说明见 [`docs/notes/connection-reuse-vs-session.md`](docs/notes/connection-reuse-vs-session.md)
- 领域词见 [`CONTEXT.md`](CONTEXT.md)

## 特性

- 复用系统 `ssh` / `scp` 与 `~/.ssh/config`（含跳板、密钥、端口）
- 同 Host 多次调用复用 **SSH 握手**（ControlMaster），不是交互 Session
- 子命令：`hosts` · `exec` · `get` · `put`
- stdout **始终为单个 JSON**，便于 Agent 解析
- CLI 层强制：命令黑名单、写路径白名单、超时与大小上限

## 安装

**推荐：GitHub Release 预编译二进制**（不需要 Go）。在 [Releases](https://github.com/CodingOX/ssh-remote/releases) 选择已发布的 `vMAJOR.MINOR.PATCH`，下载与本机匹配的归档和同页的 `SHA256SUMS`，校验后安装：

```bash
# 以 v0.1.0、macOS Apple Silicon 为例；版本与平台必须替换为 Release 中实际存在的资产。
VERSION=v0.1.0
PLATFORM=darwin_arm64
ARCHIVE="ssh-remote_${VERSION}_${PLATFORM}.tar.gz"
curl -fLO "https://github.com/CodingOX/ssh-remote/releases/download/${VERSION}/${ARCHIVE}"
curl -fLO "https://github.com/CodingOX/ssh-remote/releases/download/${VERSION}/SHA256SUMS"
grep " ${ARCHIVE}$" SHA256SUMS | shasum -a 256 -c -
tar -xzf "${ARCHIVE}"
mkdir -p "${HOME}/.local/bin"
install -m 0755 "ssh-remote_${VERSION}_${PLATFORM}/ssh-remote" "${HOME}/.local/bin/ssh-remote"
"${HOME}/.local/bin/ssh-remote" version
```

> Linux 通常使用 `sha256sum -c -` 替代 `shasum -a 256 -c -`。若 `~/.local/bin` 不在 PATH，请自行加入 shell 配置后重开终端。仅下载 Release 页面实际列出的资产；不要猜测版本、平台名或校验和。

**备选：Go 安装**（需要 Go 1.22+）：

```bash
go install github.com/CodingOX/ssh-remote/cmd/ssh-remote@latest
ssh-remote version
```

**固定源码版本构建**（需要 Go 1.22+）：

```bash
git clone https://github.com/CodingOX/ssh-remote.git
cd ssh-remote
git checkout <verified-tag-or-commit>
go build -o bin/ssh-remote ./cmd/ssh-remote
./bin/ssh-remote version
```

需要本机已安装 OpenSSH 客户端（`ssh`、`scp`）。安装后先运行 `ssh-remote doctor <host>`：`result.ssh_binary` 与 `result.scp_binary` 都为 `true` 才满足运行前置条件。

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

NPM 多平台二进制包装、审计等见 Spec §11。交互 Session 不在本产品范围，见 [ADR-0003](docs/adr/0003-no-session-reuse-daily-ops.md)。

## License

MIT
