// Package app 实现 hosts/exec/get/put 子命令业务（对齐 Spec §4）。
package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
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
		OK:        false,
		Action:    action,
		Retriable: response.RetriableFor(code, false),
		Error:     &response.ErrorBody{Code: code, Message: msg},
		Meta:      baseMeta(host, timeout, false),
		Result:    response.MustResult(result),
	}
}

func okEnv(action, host string, timeout time.Duration, truncated bool, result any) response.Envelope {
	return response.Envelope{
		OK:        true,
		Action:    action,
		Retriable: response.RetriableFor("", true),
		Error:     nil,
		Meta:      baseMeta(host, timeout, truncated),
		Result:    response.MustResult(result),
	}
}

// denyRemoteReadWithoutHome 在尚未解析远端 $HOME 时，对明确命中 read_denylist 的 ~/ 路径本地拒读。
func denyRemoteReadWithoutHome(remotePath string) bool {
	if strings.HasPrefix(remotePath, "~/.ssh/") || remotePath == "~/.ssh" {
		return true
	}
	if strings.HasSuffix(remotePath, "authorized_keys") ||
		strings.HasSuffix(remotePath, "/authorized_keys") {
		return true
	}
	return false
}

func userHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func repoRootDir() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

// classifyTransferError 根据 scp/ssh 非零退出时的 stderr 区分 connect 与 remote_fs。
// 无路径上下文的 Permission denied（认证失败）算 connect；scp/mkdir 带路径的才算 remote_fs。
func classifyTransferError(exitCode int, stderr string) string {
	if exitCode == 0 {
		return ""
	}
	if isConnectStderr(stderr) {
		return response.CodeConnect
	}
	if isRemoteFSStderr(stderr) {
		return response.CodeRemoteFS
	}
	return response.CodeConnect
}

func isConnectStderr(stderr string) bool {
	return strings.Contains(stderr, "Connection refused") ||
		strings.Contains(stderr, "Could not resolve") ||
		strings.Contains(stderr, "Host key verification failed") ||
		strings.Contains(stderr, "Network is unreachable") ||
		strings.Contains(stderr, "Permission denied (publickey") ||
		strings.Contains(stderr, "Permission denied, please try again")
}

func isRemoteFSStderr(stderr string) bool {
	if strings.Contains(stderr, "No such file") ||
		strings.Contains(stderr, "not a regular file") ||
		strings.Contains(stderr, "Is a directory") {
		return true
	}
	// 带路径上下文的权限错误（scp: <path>: Permission denied / mkdir: ... Permission denied）
	if strings.Contains(stderr, "Permission denied") &&
		(strings.Contains(stderr, "scp:") || strings.Contains(stderr, "mkdir:")) {
		return true
	}
	return false
}

// parentDirPOSIX 返回远端路径的 POSIX 父目录（保留 ~/ 形式）。
func parentDirPOSIX(remotePath string) string {
	remotePath = strings.ReplaceAll(remotePath, "\\", "/")
	if remotePath == "" {
		return ""
	}
	dir := path.Dir(remotePath)
	if dir == "." {
		return ""
	}
	return dir
}

// putRunner 供 Put 调用的 runner 入口，测试时可替换。
var putRunner = struct {
	RemoteHome func(context.Context, runner.Options, string) (string, error)
	SSHExec    func(context.Context, runner.Options, string, string) (*runner.ExecResult, error)
	SCPPut     func(context.Context, runner.Options, string, string, string) (*runner.ExecResult, error)
}{
	RemoteHome: runner.RemoteHome,
	SSHExec:    runner.SSHExec,
	SCPPut:     runner.SCPPut,
}

// expandRemotePath 将 ~/ 前缀展开为远端绝对路径（与 scp 目标一致）。
func expandRemotePath(remotePath, remoteHome string) string {
	if strings.HasPrefix(remotePath, "~/") && remoteHome != "" {
		rest := strings.TrimPrefix(remotePath, "~/")
		return strings.TrimRight(remoteHome, "/") + "/" + rest
	}
	return remotePath
}

// ensureRemoteParent 在白名单内为目标路径创建父目录；失败时返回非 nil Envelope。
func ensureRemoteParent(ctx context.Context, c *Config, host, remotePath, remoteHome string) *response.Envelope {
	parent := parentDirPOSIX(remotePath)
	// 根目录或无父目录时无需 mkdir
	if parent == "" || parent == "/" || parent == "~" {
		return nil
	}
	if err := c.Policy.CheckWritePath(parent, remoteHome); err != nil {
		to := c.timeout()
		env := fail("put", host, response.CodePolicyDenied, err.Error(), to, nil)
		return &env
	}
	mkdirPath := expandRemotePath(parent, remoteHome)
	cmd := fmt.Sprintf("mkdir -p -- %s", runner.ShellQuote(mkdirPath))
	res, err := putRunner.SSHExec(ctx, c.runnerOpt(), host, cmd)
	if err != nil {
		to := c.timeout()
		env := fail("put", host, response.CodeInternal, err.Error(), to, nil)
		return &env
	}
	if res.TimedOut {
		to := c.timeout()
		env := fail("put", host, response.CodeTimeout, "mkdir timed out", to, nil)
		return &env
	}
	if res.ExitCode != 0 {
		to := c.timeout()
		code := classifyTransferError(res.ExitCode, res.Stderr)
		env := fail("put", host, code,
			fmt.Sprintf("mkdir failed: %s", strings.TrimSpace(res.Stderr)), to, nil)
		return &env
	}
	return nil
}

// policySnapshot 将生效策略序列化为冻结 JSON 形状（空切片保留键）。
func policySnapshot(p *policy.Policy, policyPath string) map[string]any {
	cmdAllow := p.CommandAllowlist
	if cmdAllow == nil {
		cmdAllow = []string{}
	}
	cmdDeny := p.CommandDenylist
	if cmdDeny == nil {
		cmdDeny = []string{}
	}
	writeAllow := p.WriteAllowlist
	if writeAllow == nil {
		writeAllow = []string{}
	}
	readDeny := p.ReadDenylist
	if readDeny == nil {
		readDeny = []string{}
	}
	localDeny := p.LocalDenylist
	if localDeny == nil {
		localDeny = []string{}
	}
	return map[string]any{
		"policy_path":        policyPath,
		"command_timeout_ms": p.CommandTimeoutMs,
		"max_output_bytes":   p.MaxOutputBytes,
		"max_file_bytes":     p.MaxFileBytes,
		"max_command_chars":  p.MaxCommandChars,
		"read_only":          p.ReadOnly,
		"command_denylist":   cmdDeny,
		"command_allowlist":  cmdAllow,
		"write_allowlist":    writeAllow,
		"read_denylist":      readDeny,
		"local_denylist":     localDeny,
	}
}

// ensureEffectivePolicy 保证 cfg 上有生效策略与 policy_path（文件缺失时用内置默认）。
func (c *Config) ensureEffectivePolicy() (*policy.Policy, string, error) {
	path := c.PolicyPath
	if path == "" {
		path = policy.DefaultPolicyPath()
	}
	if c.Policy == nil {
		p, err := policy.Load(path)
		if err != nil {
			return nil, path, err
		}
		c.Policy = p
	}
	if c.PolicyPath == "" {
		c.PolicyPath = path
	}
	return c.Policy, c.PolicyPath, nil
}

// ShowPolicy 返回当前生效策略快照（不回显密钥；denylist 为完整 pattern 列表）。
func ShowPolicy(c *Config) response.Envelope {
	p, path, err := c.ensureEffectivePolicy()
	if err != nil {
		to := time.Duration(policy.Default().CommandTimeoutMs) * time.Millisecond
		return fail("policy", "", response.CodeInternal, err.Error(), to, nil)
	}
	to := c.timeout()
	return okEnv("policy", "", to, false, policySnapshot(p, path))
}

// doctorRunner 供 Doctor 调用的 runner 入口，测试时可替换。
var doctorRunner = struct {
	SSHExec func(context.Context, runner.Options, string, string) (*runner.ExecResult, error)
}{
	SSHExec: runner.SSHExec,
}

// doctorResult 诊断报告冻结形状（空字段保留键）。
func doctorResult(host string, cfg *Config, entries []sshconfig.HostEntry) map[string]any {
	sshPath, _ := exec.LookPath("ssh")
	scpPath, _ := exec.LookPath("scp")
	sshOK := sshPath != ""
	scpOK := scpPath != ""

	var match *sshconfig.HostEntry
	for i := range entries {
		if entries[i].Host == host {
			match = &entries[i]
			break
		}
	}
	hostFound := match != nil

	result := map[string]any{
		"ssh_binary":  sshOK,
		"scp_binary":  scpOK,
		"host_found":  hostFound,
		"hostname":    "",
		"user":        "",
		"port":        "",
		"proxy_jump":  "",
		"policy_ok":   cfg.Policy != nil,
		"mux_enabled": runner.MuxEnabled(runner.Options{}),
	}
	if match != nil {
		result["hostname"] = match.HostName
		result["user"] = match.User
		result["port"] = match.Port
		result["proxy_jump"] = match.ProxyJump
	}
	return result
}

// probeHost 在 host 已配置且本机有 ssh 时执行 `true` 探测连通性。
func probeHost(ctx context.Context, c *Config, host string, result map[string]any) {
	sshOK, _ := result["ssh_binary"].(bool)
	hostFound, _ := result["host_found"].(bool)
	if !hostFound || !sshOK {
		return
	}
	res, err := doctorRunner.SSHExec(ctx, c.runnerOpt(), host, "true")
	probe := map[string]any{}
	if err != nil {
		probe["probe_ok"] = false
		probe["probe_error_code"] = response.CodeInternal
		probe["message"] = "ssh exec failed"
		result["probe"] = probe
		return
	}
	if res.TimedOut {
		probe["probe_ok"] = false
		probe["probe_error_code"] = response.CodeTimeout
		probe["message"] = "probe timed out"
		result["probe"] = probe
		return
	}
	if res.ConnectErr && res.ExitCode == 255 {
		probe["probe_ok"] = false
		probe["probe_error_code"] = response.CodeConnect
		probe["message"] = "ssh connect failed"
		result["probe"] = probe
		return
	}
	if res.ExitCode != 0 {
		probe["probe_ok"] = false
		probe["probe_error_code"] = response.CodeRemoteExit
		probe["message"] = fmt.Sprintf("remote exit code %d", res.ExitCode)
		result["probe"] = probe
		return
	}
	probe["probe_ok"] = true
	result["probe"] = probe
}

// Doctor 对指定 Host 做非破坏性诊断（不读 IdentityFile、不回显密钥）。
func Doctor(c *Config, host string) response.Envelope {
	to := time.Duration(policy.Default().CommandTimeoutMs) * time.Millisecond
	if c.Policy != nil {
		to = c.timeout()
	}
	if host == "" {
		return fail("doctor", "", response.CodeUsage, "host is required", to, doctorResult("", c, nil))
	}

	configPath := c.SSHConfig
	if configPath == "" {
		configPath = sshconfig.DefaultConfigPath()
	}
	entries, err := sshconfig.ListHostEntries(configPath)
	if err != nil {
		return fail("doctor", host, response.CodeInternal, err.Error(), to, doctorResult(host, c, nil))
	}

	result := doctorResult(host, c, entries)
	if !result["host_found"].(bool) {
		return fail("doctor", host, response.CodeUsage, "host not in ssh config", to, result)
	}

	ctx := context.Background()
	probeHost(ctx, c, host, result)
	return okEnv("doctor", host, to, false, result)
}

// Hosts 列出 Host 别名及其寻址字段（User/HostName/Port/ProxyJump，不探测网络）。
func Hosts(c *Config) response.Envelope {
	to := c.timeout()
	path := c.SSHConfig
	if path == "" {
		path = sshconfig.DefaultConfigPath()
	}
	entries, err := sshconfig.ListHostEntries(path)
	if err != nil {
		return fail("hosts", "", response.CodeInternal, err.Error(), to, nil)
	}
	// 空字段保留键并用空字符串，便于 Agent 稳定解析。
	hosts := make([]map[string]string, 0, len(entries))
	for _, e := range entries {
		hosts = append(hosts, map[string]string{
			"name":       e.Host,
			"hostname":   e.HostName,
			"user":       e.User,
			"port":       e.Port,
			"proxy_jump": e.ProxyJump,
		})
	}
	return okEnv("hosts", "", to, false, map[string]any{
		"hosts":       hosts,
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
	// 只读策略：仅允许冻结的只读命令集合
	if err := c.Policy.CheckReadOnlyCommand(userCmd); err != nil {
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

	homeDir := userHomeDir()
	if err := c.Policy.CheckLocalDest(localPath, homeDir, repoRootDir()); err != nil {
		return fail("get", host, response.CodePolicyDenied, err.Error(), to, nil)
	}

	// 读路径策略：绝对路径纯本地判定；~/ 敏感路径不解析 home 即拒，其余才 ssh 取 $HOME
	remoteHome := ""
	if strings.HasPrefix(remotePath, "~/") || remotePath == "~" {
		if denyRemoteReadWithoutHome(remotePath) {
			return fail("get", host, response.CodePolicyDenied,
				fmt.Sprintf("remote read path denied: %s", remotePath), to, nil)
		}
		home, err := runner.RemoteHome(context.Background(), c.runnerOpt(), host)
		if err != nil {
			return fail("get", host, response.CodeConnect, "resolve remote home: "+err.Error(), to, nil)
		}
		remoteHome = home
	}
	if err := c.Policy.CheckReadPath(remotePath, remoteHome); err != nil {
		return fail("get", host, response.CodePolicyDenied, err.Error(), to, nil)
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

	// remotePath 含空格时由 runner 经 exec.Command 单 argv 传递，无需 shell 拼接。
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
		code := classifyTransferError(res.ExitCode, res.Stderr)
		return fail("get", host, code,
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
	// 只读策略：拒绝一切写操作，不解析远端路径、不 scp
	if err := c.Policy.CheckReadOnly(); err != nil {
		return fail("put", host, response.CodePolicyDenied, err.Error(), to, nil)
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

	if err := c.Policy.CheckLocalSource(localPath, userHomeDir()); err != nil {
		return fail("put", host, response.CodePolicyDenied, err.Error(), to, nil)
	}

	ctx := context.Background()
	opt := c.runnerOpt()

	// 写路径策略：绝对路径纯本地判定；~/ 路径才解析远端 $HOME
	remoteHome := ""
	if strings.HasPrefix(remotePath, "~/") || remotePath == "~" {
		home, err := putRunner.RemoteHome(ctx, opt, host)
		if err != nil {
			return fail("put", host, response.CodeConnect, "resolve remote home: "+err.Error(), to, nil)
		}
		remoteHome = home
	}
	if err := c.Policy.CheckWritePath(remotePath, remoteHome); err != nil {
		return fail("put", host, response.CodePolicyDenied, err.Error(), to, nil)
	}

	if env := ensureRemoteParent(ctx, c, host, remotePath, remoteHome); env != nil {
		return *env
	}

	// scp 目标：若用户给 ~/，展开为绝对路径更稳
	scpRemote := expandRemotePath(remotePath, remoteHome)

	res, err := putRunner.SCPPut(ctx, opt, host, localPath, scpRemote)
	if err != nil {
		return fail("put", host, response.CodeInternal, err.Error(), to, nil)
	}
	if res.TimedOut {
		return fail("put", host, response.CodeTimeout, "scp timed out", to, nil)
	}
	if res.ExitCode != 0 {
		code := classifyTransferError(res.ExitCode, res.Stderr)
		return fail("put", host, code,
			fmt.Sprintf("scp failed: %s", strings.TrimSpace(res.Stderr)), to, nil)
	}

	abs, _ := filepath.Abs(localPath)
	return okEnv("put", host, to, false, map[string]any{
		"remote_path": scpRemote,
		"local_path":  abs,
		"bytes":       fi.Size(),
	})
}
