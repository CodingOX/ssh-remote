// Package audit 提供轻量审计日志：命令执行记录以 JSONL 追加写入本地文件，
// 写前惰性轮转（超阈值改名留档、删最老），磁盘占用有上限，无需定时器。
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// 默认轮转参数（单条记录约 300B，10MiB ≈ 3 万条，5 份封顶 ~50MiB）。
const (
	// DefaultMaxSize 触发轮转的文件大小阈值（字节）。
	DefaultMaxSize int64 = 10 << 20 // 10 MiB
	// DefaultKeep 轮转后保留的历史文件份数（.1 最新 … .N 最老，超限删除）。
	DefaultKeep = 5
)

// Record 一条审计记录。字段对齐 response.Meta 的 snake_case JSON 风格；
// Cmd 必须为 Redact 后的脱敏命令，审计日志本身不落敏感原文。
type Record struct {
	TS        string `json:"ts"`                  // ISO8601 本地时间
	Action    string `json:"action"`              // exec / get / put / hosts / doctor
	Host      string `json:"host"`                // 目标主机（hosts/doctor 可为空）
	Cmd       string `json:"cmd"`                 // 脱敏后的命令或操作描述
	ExitCode  int    `json:"exit_code"`           // 进程退出码
	DurMs     int64  `json:"dur_ms"`              // 耗时（毫秒）
	Truncated bool   `json:"truncated,omitempty"` // 输出是否被截断
	Err       string `json:"err,omitempty"`       // 错误消息（失败时）
}

// Append 追加一条记录到 path（JSONL，一行一条）。
// 写入前惰性检查：当前文件超过 maxSize 即轮转（见 rotate），
// 语义：maxSize>0 才启用轮转（0=禁用，文件可无限增长）；keep 为保留的历史
// 份数（.1 最新 … .N 最老，超限删除），keep=0 表示轮转时不保留历史。
// 默认值（DefaultMaxSize / DefaultKeep）由调用方显式传入，本函数不做隐式替换。
func Append(path string, rec Record, maxSize int64, keep int) error {
	// 先确保目录存在，再检查大小，避免首次写入时 Stat 误判
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("audit mkdir: %w", err)
	}
	if maxSize > 0 {
		if fi, err := os.Stat(path); err == nil && fi.Size() >= maxSize {
			rotate(path, keep)
		}
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("audit marshal: %w", err)
	}
	// O_APPEND：单进程追加，单行写入原子，轮转改名不影响已打开句柄
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("audit open: %w", err)
	}
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		return fmt.Errorf("audit chmod: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("audit write: %w", err)
	}
	return nil
}

// rotate 就地轮转：删最老（.keep）→ .1… 依次后移 → 当前文件变 .1，
// 新文件在下次 Append 时自动创建。keep<=0 表示不保留历史，直接删当前文件。
func rotate(path string, keep int) {
	if keep <= 0 {
		_ = os.Remove(path)
		return
	}
	_ = os.Remove(fmt.Sprintf("%s.%d", path, keep))
	for i := keep - 1; i >= 1; i-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", path, i), fmt.Sprintf("%s.%d", path, i+1))
	}
	_ = os.Rename(path, path+".1")
}

// secretWord 敏感词复合元（大小写不敏感）：password/token/secret 等值语义词，
// 及 api_key/access-key/private_key 等复合键。刻意不含裸 "key"（如 --keyfile
// 路径、ssh-keygen 等），避免误伤命令可读性。
const secretWord = `(?:password|passwd|pwd|token|secret|credential|api[_-]?key|access[_-]?key|secret[_-]?key|private[_-]?key)`

// secretValue 匹配三种值形态：双引号、单引号、裸词（不含引号与空白）。
const secretValue = `("[^"]*"|'[^']*'|\S+)`

// redactPatterns 依次应用的脱敏规则，顺序敏感（先长选项后短形态）。
var redactPatterns = []struct {
	re  *regexp.Regexp
	rep string
}{
	// 1) 长选项赋值/空格值：--password=hunter2 / --token abc
	//    仅匹配 "词首 --" 前缀，`ssh-keygen`、`--keyfile` 等不含 -- 前缀的不命中；
	//    值统一替换为 "***"（保留引号框架，忠实原文格式）
	{regexp.MustCompile(`(?i)(--[a-z0-9-]*` + secretWord + `[a-z0-9-]*(?:=|\s+))(` + secretValue + `)`), `${1}"***"`},
	// 2) sshpass 短选项：sshpass -p secret ssh host（-p 太通用，仅此场景脱敏）
	{regexp.MustCompile(`(?i)(sshpass\s+-p\s+)` + secretValue), `${1}"***"`},
	// 3) URL query 参数：?token=abc&page=2（值到 & 或空白为止，不带引号以免破坏 URL）
	{regexp.MustCompile(`(?i)([?&](?:` + secretWord + `|auth|authorization)=)[^&\s"']*`), `${1}***`},
	// 4) HTTP 头：Authorization: Bearer xxx / 独立 Bearer/Basic 串（≥8 字符避免误伤）
	{regexp.MustCompile(`(?i)(authorization\s*:\s*(?:bearer|basic)\s+)[A-Za-z0-9._~+/=-]+`), `${1}***`},
	{regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]{8,}`), `$1 ***`},
	// 5) 环境变量赋值：TOKEN=secret / export AWS_SECRET_ACCESS_KEY=abc123。
	//    变量名可以就是敏感词（TOKEN=），也可以带前缀/后缀（AWS_SECRET_ACCESS_KEY=）。
	//    前缀限定为行首/分号/空白/&&，不含单个 &，避免规则 3 处理后的 URL
	//    &token=*** 被二次改写成 &token="***"。
	{regexp.MustCompile(`(?im)(^|[;\s]|&&)(\s*)((?:[A-Za-z_][A-Z0-9_]*)?` + secretWord + `[A-Z0-9_]*=)(` + secretValue + `)`), `${1}${2}${3}"***"`},
}

// Redact 对命令做保守脱敏：识别常见敏感词形态并替换值为 ***。
// 宁可多脱敏（审计只追溯，不依赖命令全文），但避免误伤通用词（--port、-p、keyfile）。
// 不命中任何规则时原样返回。
func Redact(cmd string) string {
	out := cmd
	for _, p := range redactPatterns {
		out = p.re.ReplaceAllString(out, p.rep)
	}
	return out
}
