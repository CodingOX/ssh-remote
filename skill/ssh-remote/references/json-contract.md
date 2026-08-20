# JSON 合同

每次 `ssh-remote` 调用均向 stdout 输出一个 JSON 对象；stderr 仅供人类查看，自动处理时不依赖它。

## 通用字段

| 字段 | 含义 |
| --- | --- |
| `ok` | 操作是否成功 |
| `action` | `version`、`hosts`、`doctor`、`policy`、`exec`、`get` 或 `put` |
| `result` | 随 action 变化的结果 |
| `error.code` | 失败分类 |
| `retriable` | 是否适合原参重试 |
| `meta.truncated` | 输出是否截断 |

## 失败分类

| `error.code` | `retriable` | 处理方式 |
| --- | --- | --- |
| `connect` | true | 检查 SSH 配置、认证与跳板，可短暂重试 |
| `timeout` | true | 缩小任务、提高超时后重试 |
| `remote_fs` | false | 修正远端路径、权限或文件类型 |
| `policy_denied` | false | 阅读错误信息，改命令/路径或经用户确认调整策略 |
| `remote_exit` | false | 分析远端命令的 stdout、stderr、exit_code |
| `usage` / `local_fs` / `internal` | false | 修正本地输入或报告 CLI 异常 |

`connect` 与 `timeout` 的进程退出码都可能为 4，必须以 `error.code` 区分。`remote_fs` 表示已成功连接，不能当作 SSH 认证问题处理。

## result 摘要

- `version`：`result.version` 是语义化版本。
- `hosts`：`result.hosts` 是对象数组；使用 `name` 作为后续命令的 Host 参数。另含 `config_path`。
- `doctor`：含本机 ssh/scp 检测、`host_found`、解析后的连接字段、`policy_ok`、`mux_enabled` 和可选 `probe`。
- `policy`：含生效策略路径与所有策略值。
- `exec`：含 `stdout`、`stderr`、`exit_code`、`command`，可选 `workdir`；远端非零退出时仍可能提供结果。
- `get` / `put`：返回路径、字节数等传输元数据，不将文件全文放入 JSON。

退出码：0 成功、1 用法或本地错误、2 策略拒绝、3 远端非零、4 连接或超时、5 内部错误、6 远端文件系统错误。
