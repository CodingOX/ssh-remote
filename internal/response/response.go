// Package response 定义 CLI 统一 JSON 合同与进程退出码（见 Spec §5–§6）。
package response

import (
	"encoding/json"
	"io"
	"os"
)

// 进程退出码（Spec §6）
const (
	ExitOK       = 0
	ExitUsage    = 1 // usage / local_fs
	ExitPolicy   = 2 // policy_denied
	ExitRemote   = 3 // remote_exit
	ExitConnect  = 4 // connect / timeout
	ExitInternal = 5 // internal
)

// 机器可读错误码（Spec §5.4）
const (
	CodeUsage        = "usage"
	CodePolicyDenied = "policy_denied"
	CodeRemoteExit   = "remote_exit"
	CodeConnect      = "connect"
	CodeTimeout      = "timeout"
	CodeLocalFS      = "local_fs"
	CodeInternal     = "internal"
)

// ErrorBody 失败时的 error 对象。
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Meta 运行元数据。
type Meta struct {
	Version   string  `json:"version"`
	Host      *string `json:"host"` // hosts 子命令为 null
	TimeoutMs int64   `json:"timeout_ms"`
	Truncated bool    `json:"truncated,omitempty"`
}

// Envelope 每次调用 stdout 输出的唯一 JSON 对象。
type Envelope struct {
	OK     bool            `json:"ok"`
	Action string          `json:"action"`
	Error  *ErrorBody      `json:"error"`
	Meta   Meta            `json:"meta"`
	Result json.RawMessage `json:"result"`
}

// ExitForCode 将 error.code 映射为进程退出码。
func ExitForCode(code string) int {
	switch code {
	case "":
		return ExitOK
	case CodeUsage, CodeLocalFS:
		return ExitUsage
	case CodePolicyDenied:
		return ExitPolicy
	case CodeRemoteExit:
		return ExitRemote
	case CodeConnect, CodeTimeout:
		return ExitConnect
	default:
		return ExitInternal
	}
}

// Write 将 envelope 写到 w（通常 os.Stdout），末尾换行。
func Write(w io.Writer, env Envelope) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(env)
}

// WriteAndExit 写 JSON 并以对应码退出（供 main 使用）。
func WriteAndExit(env Envelope) {
	_ = Write(os.Stdout, env)
	if env.OK {
		os.Exit(ExitOK)
	}
	if env.Error != nil {
		os.Exit(ExitForCode(env.Error.Code))
	}
	os.Exit(ExitInternal)
}

// HostPtr 辅助：空串表示 null host。
func HostPtr(host string) *string {
	if host == "" {
		return nil
	}
	return &host
}

// MustResult 将任意可 JSON 序列化值转为 RawMessage。
func MustResult(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return b
}
