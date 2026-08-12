// Package app 实现 hosts/exec/get/put 子命令业务（对齐 Spec §4）。
package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/CodingOX/ssh-remote/internal/policy"
	"github.com/CodingOX/ssh-remote/internal/response"
	"github.com/CodingOX/ssh-remote/internal/runner"
	"github.com/CodingOX/ssh-remote/internal/sshconfig"
	"github.com/CodingOX/ssh-remote/internal/version"
)

// Config 一次 CLI 调用的全局选项。
type Config struct {
	SSHConfig  string
	PolicyPath string
	Timeout    time.Duration // 0 表示用 policy
	Workdir    string
	Policy     *policy.Policy
}

// LoadPolicy 加载策略到 cfg。
func (c *Config) LoadPolicy() error {
	path := c.PolicyPath
	if path == "" {
		path = policy.DefaultPolicyPath()
	}
	p, err := policy.Load(path)
	if err != nil {
		return err
	}
	c.Policy = p
	// 记录实际使用的路径（文件可不存在）
	c.PolicyPath = path
	return nil
}

func (c *Config) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return time.Duration(c.Policy.CommandTimeoutMs) * time.Millisecond
}

func (c *Config) runnerOpt() runner.Options {
	return runner.Options{
		SSHConfig: c.SSHConfig,
		Timeout:   c.timeout(),
		MaxStdout: c.Policy.MaxOutputBytes,
		MaxStderr: c.Policy.MaxOutputBytes,
	}
}

func baseMeta(host string, timeout time.Duration, truncated bool) response.Meta {
	return response.Meta{
		Version:   version.Version,
		Host:      response.HostPtr(host),
		TimeoutMs: timeout.Milliseconds(),
		Truncated: truncated,
	}
}

func fail(action, host, code, msg string, timeout time.Duration, result any) response.Envelope {
	return response.Envelope{
		OK:     false,
		Action: action,
		Error:  &response.ErrorBody{Code: code, Message: msg},
		Meta:   baseMeta(host, timeout, false),
		Result: response.MustResult(result),
	}
}

func okEnv(action, host string, timeout time.Duration, truncated bool, result any) response.Envelope {
	return response.Envelope{
		OK:     true,
		Action: action,
		Error:  nil,
		Meta:   baseMeta(host, timeout, truncated),
		Result: response.MustResult(result),
	}
}

// Hosts 列出 Host 别名。
func Hosts(c *Config) response.Envelope {
	to := c.timeout()
	path := c.SSHConfig
	if path == "" {
		path = sshconfig.DefaultConfigPath()
	}
	list, err := sshconfig.ListHosts(path)
	if err != nil {
		return fail("hosts", "", response.CodeInternal, err.Error(), to, nil)
	}
	return okEnv("hosts", "", to, false, map[string]any{
		"hosts":       list,
		"config_path": path,
	})
}

// Exec 远程执行命令。
func Exec(c *Config, host string, commandParts []string) response.Envelope {
	to := c.timeout()
	if host == "" {
		return fail("exec", "", response.CodeUsage, "host is required", to, nil)
	}
	if len(commandParts) == 0 {
		return fail("exec", host, response.CodeUsage, "command is required", to, nil)
	}
	userCmd := strings.Join(commandParts, " ")
	finalCmd := runner.WrapWorkdir(c.Workdir, userCmd)

	if err := c.Policy.CheckCommand(userCmd, finalCmd); err != nil {
		return fail("exec", host, response.CodePolicyDenied, err.Error(), to, nil)
	}

	ctx := context.Background()
	res, err := runner.SSHExec(ctx, c.runnerOpt(), host, finalCmd)
	if err != nil {
		return fail("exec", host, response.CodeInternal, err.Error(), to, nil)
	}

	var workdir any
	if c.Workdir != "" {
		workdir = c.Workdir
	} else {
		workdir = nil
	}
	result := map[string]any{
		"command":   finalCmd,
		"exit_code": res.ExitCode,
		"stdout":    res.Stdout,
		"stderr":    res.Stderr,
		"workdir":   workdir,
	}

	if res.TimedOut {
		env := fail("exec", host, response.CodeTimeout, "command timed out", to, result)
		env.Meta.Truncated = res.Truncated
		return env
	}
	if res.ConnectErr && res.ExitCode == 255 {
		env := fail("exec", host, response.CodeConnect,
			fmt.Sprintf("ssh connect failed: %s", strings.TrimSpace(res.Stderr)), to, result)
		env.Meta.Truncated = res.Truncated
		return env
	}
	if res.ExitCode != 0 {
		env := fail("exec", host, response.CodeRemoteExit,
			fmt.Sprintf("remote exit code %d", res.ExitCode), to, result)
		env.Meta.Truncated = res.Truncated
		return env
	}
	return okEnv("exec", host, to, res.Truncated, result)
}

// Get 下载远程文件。
func Get(c *Config, host, remotePath, localPath string) response.Envelope {
	to := c.timeout()
	if host == "" || remotePath == "" {
		return fail("get", host, response.CodeUsage, "host and remote-path are required", to, nil)
	}
	if localPath == "" {
		localPath = filepath.Base(remotePath)
	}

	// 传输前尽力探测远端大小
	ctx := context.Background()
	opt := c.runnerOpt()
	sizeCmd := fmt.Sprintf("stat -c %%s %s 2>/dev/null || stat -f %%z %s 2>/dev/null",
		runner.ShellQuote(remotePath), runner.ShellQuote(remotePath))
	if st, err := runner.SSHExec(ctx, opt, host, sizeCmd); err == nil && st.ExitCode == 0 {
		var sz int64
		if _, scanErr := fmt.Sscan(strings.TrimSpace(st.Stdout), &sz); scanErr == nil {
			if sz > c.Policy.MaxFileBytes {
				return fail("get", host, response.CodePolicyDenied,
					fmt.Sprintf("remote file exceeds max_file_bytes (%d > %d)", sz, c.Policy.MaxFileBytes), to, nil)
			}
		}
	}

	res, err := runner.SCPGet(ctx, opt, host, remotePath, localPath)
	if err != nil {
		return fail("get", host, response.CodeInternal, err.Error(), to, nil)
	}
	if res.TimedOut {
		_ = os.Remove(localPath)
		return fail("get", host, response.CodeTimeout, "scp timed out", to, nil)
	}
	if res.ExitCode != 0 {
		_ = os.Remove(localPath)
		return fail("get", host, response.CodeConnect,
			fmt.Sprintf("scp failed: %s", strings.TrimSpace(res.Stderr)), to, nil)
	}

	fi, err := os.Stat(localPath)
	if err != nil {
		return fail("get", host, response.CodeLocalFS, err.Error(), to, nil)
	}
	if fi.Size() > c.Policy.MaxFileBytes {
		_ = os.Remove(localPath)
		return fail("get", host, response.CodePolicyDenied,
			fmt.Sprintf("downloaded file exceeds max_file_bytes (%d > %d)", fi.Size(), c.Policy.MaxFileBytes), to, nil)
	}

	abs, _ := filepath.Abs(localPath)
	return okEnv("get", host, to, false, map[string]any{
		"remote_path": remotePath,
		"local_path":  abs,
		"bytes":       fi.Size(),
	})
}

// Put 上传本地文件到白名单路径。
func Put(c *Config, host, localPath, remotePath string) response.Envelope {
	to := c.timeout()
	if host == "" || localPath == "" || remotePath == "" {
		return fail("put", host, response.CodeUsage, "host, local-path and remote-path are required", to, nil)
	}

	fi, err := os.Stat(localPath)
	if err != nil {
		return fail("put", host, response.CodeLocalFS, err.Error(), to, nil)
	}
	if !fi.Mode().IsRegular() {
		return fail("put", host, response.CodeLocalFS, "local path is not a regular file", to, nil)
	}
	if fi.Size() > c.Policy.MaxFileBytes {
		return fail("put", host, response.CodePolicyDenied,
			fmt.Sprintf("local file exceeds max_file_bytes (%d > %d)", fi.Size(), c.Policy.MaxFileBytes), to, nil)
	}

	ctx := context.Background()
	opt := c.runnerOpt()

	// 写路径策略：绝对路径纯本地判定；~/ 路径才解析远端 $HOME
	remoteHome := ""
	if strings.HasPrefix(remotePath, "~/") || remotePath == "~" {
		home, err := runner.RemoteHome(ctx, opt, host)
		if err != nil {
			return fail("put", host, response.CodeConnect, "resolve remote home: "+err.Error(), to, nil)
		}
		remoteHome = home
	}
	if err := c.Policy.CheckWritePath(remotePath, remoteHome); err != nil {
		return fail("put", host, response.CodePolicyDenied, err.Error(), to, nil)
	}

	// scp 目标：若用户给 ~/，展开为绝对路径更稳
	scpRemote := remotePath
	if strings.HasPrefix(remotePath, "~/") && remoteHome != "" {
		rest := strings.TrimPrefix(remotePath, "~/")
		scpRemote = strings.TrimRight(remoteHome, "/") + "/" + rest
	}

	res, err := runner.SCPPut(ctx, opt, host, localPath, scpRemote)
	if err != nil {
		return fail("put", host, response.CodeInternal, err.Error(), to, nil)
	}
	if res.TimedOut {
		return fail("put", host, response.CodeTimeout, "scp timed out", to, nil)
	}
	if res.ExitCode != 0 {
		return fail("put", host, response.CodeConnect,
			fmt.Sprintf("scp failed: %s", strings.TrimSpace(res.Stderr)), to, nil)
	}

	abs, _ := filepath.Abs(localPath)
	return okEnv("put", host, to, false, map[string]any{
		"remote_path": scpRemote,
		"local_path":  abs,
		"bytes":       fi.Size(),
	})
}
