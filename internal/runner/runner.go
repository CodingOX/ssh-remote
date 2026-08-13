// Package runner 封装系统 ssh/scp 子进程调用（非交互、超时、截断）。
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// Options 一次远程调用选项。
type Options struct {
	SSHConfig string // -F 路径，空则系统默认
	Timeout   time.Duration
	// MaxStdout / MaxStderr 各自字节上限（Spec：分别上限）
	MaxStdout int
	MaxStderr int
	// DisableMux 为 true 时不传 ControlMaster 等复用参数（单测/显式回退）。
	DisableMux bool
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
//
// 连接复用由 OpenSSH ControlMaster=auto 负责：control socket 损坏时会自动新建连接，
// runner 层不做额外重试；上层若需可设 Options.DisableMux 强制单次握手。
func SSHExec(ctx context.Context, opt Options, host, command string) (*ExecResult, error) {
	if host == "" {
		return nil, fmt.Errorf("host is empty")
	}
	args := sshBaseArgs(opt)
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
	args := scpBaseArgs(opt)
	src := fmt.Sprintf("%s:%s", host, remotePath)
	args = append(args, src, localPath)
	return runCaptured(ctx, opt, "scp", args)
}

// SCPPut 上传：scp [opts] local host:remote
func SCPPut(ctx context.Context, opt Options, host, localPath, remotePath string) (*ExecResult, error) {
	args := scpBaseArgs(opt)
	dst := fmt.Sprintf("%s:%s", host, remotePath)
	args = append(args, localPath, dst)
	return runCaptured(ctx, opt, "scp", args)
}

func sshBaseArgs(opt Options) []string {
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=10",
		"-T",
	}
	args = append(args, controlMuxArgs(opt)...)
	if opt.SSHConfig != "" {
		args = append(args, "-F", opt.SSHConfig)
	}
	return args
}

func scpBaseArgs(opt Options) []string {
	// scp 通过 -o 传给底层 ssh
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=10",
		"-q",
	}
	args = append(args, controlMuxArgs(opt)...)
	if opt.SSHConfig != "" {
		args = append(args, "-F", opt.SSHConfig)
	}
	return args
}

// MuxEnabled 报告本次调用是否实际带上 ControlMaster 参数（目录不可写或 DisableMux 时为 false）。
func MuxEnabled(opt Options) bool {
	return len(controlMuxArgs(opt)) > 0
}

// controlMuxArgs 返回 OpenSSH 连接复用参数；目录不可写或 DisableMux 时省略（回退单次握手）。
func controlMuxArgs(opt Options) []string {
	if opt.DisableMux {
		return nil
	}
	dir, err := controlPathDir()
	if err != nil {
		return nil
	}
	if err := ensureControlDir(dir); err != nil {
		return nil
	}
	path := filepath.Join(dir, "cm-%C")
	return []string{
		"-o", "ControlMaster=auto",
		"-o", "ControlPersist=60s",
		"-o", "ControlPath=" + path,
	}
}

func controlPathDir() (string, error) {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		var err error
		base, err = os.UserCacheDir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(base, "ssh-remote"), nil
}

func ensureControlDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	testFile := filepath.Join(dir, ".write-test")
	f, err := os.Create(testFile)
	if err != nil {
		return err
	}
	_ = f.Close()
	return os.Remove(testFile)
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
	setProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	pgid := processGroupID(cmd)
	go func() {
		<-cctx.Done()
		if cctx.Err() == context.DeadlineExceeded {
			killByPGID(pgid, cmd)
		}
	}()

	err := cmd.Wait()
	res := &ExecResult{
		Stdout:    stdoutBuf.String(),
		Stderr:    stderrBuf.String(),
		Truncated: stdoutBuf.truncated || stderrBuf.truncated,
	}

	if cctx.Err() == context.DeadlineExceeded {
		killByPGID(pgid, cmd)
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

// setProcessGroup 让 ssh/scp 子进程成为新进程组组长，便于超时后整组清理。
func setProcessGroup(cmd *exec.Cmd) {
	if runtime.GOOS == "windows" {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func processGroupID(cmd *exec.Cmd) int {
	if runtime.GOOS == "windows" || cmd.Process == nil {
		return 0
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		return 0
	}
	return pgid
}

// killByPGID 在 context 超时后 best-effort 杀掉整棵子进程树。
// 仅杀本次 cmd 的 pgid，不 SIGKILL 其他 ssh ControlMaster。
// 共享 control socket（cm-%C 按 host 哈希）不在此删除，靠 ControlPersist 空闲回收。
func killByPGID(pgid int, cmd *exec.Cmd) {
	if runtime.GOOS == "windows" {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return
	}
	if pgid > 0 {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		return
	}
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
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
