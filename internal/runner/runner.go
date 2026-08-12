// Package runner 封装系统 ssh/scp 子进程调用（非交互、超时、截断）。
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Options 一次远程调用选项。
type Options struct {
	SSHConfig string // -F 路径，空则系统默认
	Timeout   time.Duration
	// MaxStdout / MaxStderr 各自字节上限（Spec：分别上限）
	MaxStdout int
	MaxStderr int
}

// ExecResult 远程命令结果。
type ExecResult struct {
	ExitCode  int
	Stdout    string
	Stderr    string
	Truncated bool
	// TimedOut 是否因超时被杀
	TimedOut bool
	// ConnectErr 是否更像连接/ssh 客户端失败（无远端退出码语义）
	ConnectErr bool
}

// SSHExec 执行：ssh [opts] host -- command
// command 为已拼好的远端 shell 命令字符串。
func SSHExec(ctx context.Context, opt Options, host, command string) (*ExecResult, error) {
	if host == "" {
		return nil, fmt.Errorf("host is empty")
	}
	args := sshBaseArgs(opt.SSHConfig)
	args = append(args, host, "--", command)
	return runCaptured(ctx, opt, "ssh", args)
}

// RemoteHome 通过 ssh 读取远端 $HOME。
func RemoteHome(ctx context.Context, opt Options, host string) (string, error) {
	res, err := SSHExec(ctx, opt, host, `printf %s "$HOME"`)
	if err != nil {
		return "", err
	}
	if res.TimedOut {
		return "", fmt.Errorf("timeout resolving remote home")
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("resolve remote home failed: %s", res.Stderr)
	}
	home := strings.TrimSpace(res.Stdout)
	if home == "" {
		return "", fmt.Errorf("empty remote home")
	}
	return home, nil
}

// SCPGet 下载：scp [opts] host:remote local
func SCPGet(ctx context.Context, opt Options, host, remotePath, localPath string) (*ExecResult, error) {
	args := scpBaseArgs(opt.SSHConfig)
	src := fmt.Sprintf("%s:%s", host, remotePath)
	args = append(args, src, localPath)
	return runCaptured(ctx, opt, "scp", args)
}

// SCPPut 上传：scp [opts] local host:remote
func SCPPut(ctx context.Context, opt Options, host, localPath, remotePath string) (*ExecResult, error) {
	args := scpBaseArgs(opt.SSHConfig)
	dst := fmt.Sprintf("%s:%s", host, remotePath)
	args = append(args, localPath, dst)
	return runCaptured(ctx, opt, "scp", args)
}

func sshBaseArgs(config string) []string {
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-T",
	}
	if config != "" {
		args = append(args, "-F", config)
	}
	return args
}

func scpBaseArgs(config string) []string {
	// scp 通过 -o 传给底层 ssh
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-q",
	}
	if config != "" {
		args = append(args, "-F", config)
	}
	return args
}

func runCaptured(ctx context.Context, opt Options, bin string, args []string) (*ExecResult, error) {
	if opt.Timeout <= 0 {
		opt.Timeout = 60 * time.Second
	}
	if opt.MaxStdout <= 0 {
		opt.MaxStdout = 1 << 20
	}
	if opt.MaxStderr <= 0 {
		opt.MaxStderr = 1 << 20
	}

	cctx, cancel := context.WithTimeout(ctx, opt.Timeout)
	defer cancel()

	// 仅允许静态二进制名，避免可变 command 注入（ssh / scp）
	var cmd *exec.Cmd
	switch bin {
	case "ssh":
		cmd = exec.CommandContext(cctx, "ssh", args...)
	case "scp":
		cmd = exec.CommandContext(cctx, "scp", args...)
	default:
		return nil, fmt.Errorf("unsupported binary: %s", bin)
	}
	var stdoutBuf, stderrBuf limitedBuffer
	stdoutBuf.limit = opt.MaxStdout
	stderrBuf.limit = opt.MaxStderr
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	res := &ExecResult{
		Stdout:    stdoutBuf.String(),
		Stderr:    stderrBuf.String(),
		Truncated: stdoutBuf.truncated || stderrBuf.truncated,
	}

	if cctx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
		res.ConnectErr = true
		res.ExitCode = -1
		return res, nil
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.ExitCode = ee.ExitCode()
			// ssh 自身连接失败常见 255
			if bin == "ssh" && res.ExitCode == 255 {
				res.ConnectErr = true
			}
			if bin == "scp" && res.ExitCode != 0 {
				// scp 非 0 多数为传输/连接问题
				res.ConnectErr = true
			}
			return res, nil
		}
		return res, err
	}
	res.ExitCode = 0
	return res, nil
}

// limitedBuffer 截断写入，避免撑爆内存与 Agent 上下文。
type limitedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if l.limit <= 0 {
		return l.buf.Write(p)
	}
	remain := l.limit - l.buf.Len()
	if remain <= 0 {
		l.truncated = true
		return len(p), nil
	}
	if len(p) > remain {
		_, _ = l.buf.Write(p[:remain])
		l.truncated = true
		return len(p), nil
	}
	return l.buf.Write(p)
}

func (l *limitedBuffer) String() string {
	return l.buf.String()
}

// ShellQuote 对远端路径/workdir 做单引号包裹，防止注入。
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// WrapWorkdir 生成：cd -- <dir> && <cmd>
func WrapWorkdir(workdir, command string) string {
	if workdir == "" {
		return command
	}
	return "cd -- " + ShellQuote(workdir) + " && " + command
}
