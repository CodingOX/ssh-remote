package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CodingOX/ssh-remote/internal/policy"
	"github.com/CodingOX/ssh-remote/internal/response"
	"github.com/CodingOX/ssh-remote/internal/runner"
)

// ShowPolicy 在 Default() 上应暴露 write_allowlist 含 /tmp/ 与 max_command_chars=5000。
func TestShowPolicyDefaultIncludesWriteAllowlistAndMaxCommandChars(t *testing.T) {
	cfg := &Config{
		Policy: policy.Default(),
	}
	env := ShowPolicy(cfg)
	if !env.OK {
		t.Fatalf("expected ok, got error %+v", env.Error)
	}
	if env.Action != "policy" {
		t.Fatalf("action=%q, want policy", env.Action)
	}

	var result struct {
		PolicyPath       string   `json:"policy_path"`
		CommandTimeout   int64    `json:"command_timeout_ms"`
		MaxOutputBytes   int      `json:"max_output_bytes"`
		MaxFileBytes     int64    `json:"max_file_bytes"`
		MaxCommandChars  int      `json:"max_command_chars"`
		ReadOnly         bool     `json:"read_only"`
		CommandDenylist  []string `json:"command_denylist"`
		CommandAllowlist []string `json:"command_allowlist"`
		WriteAllowlist   []string `json:"write_allowlist"`
		ReadDenylist     []string `json:"read_denylist"`
		LocalDenylist    []string `json:"local_denylist"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.MaxCommandChars != 5000 {
		t.Fatalf("max_command_chars=%d, want 5000", result.MaxCommandChars)
	}
	foundTmp := false
	for _, w := range result.WriteAllowlist {
		if w == "/tmp/" {
			foundTmp = true
			break
		}
	}
	if !foundTmp {
		t.Fatalf("write_allowlist=%v, want to include /tmp/", result.WriteAllowlist)
	}
	if result.CommandAllowlist == nil {
		t.Fatal("command_allowlist key missing")
	}
	if len(result.CommandDenylist) == 0 {
		t.Fatal("command_denylist should not be empty for defaults")
	}
}

// Doctor 对夹具 config 中的 prod-api 应 host_found=true 并填 user；未知 host 不探测。
func TestDoctorProdAPIHostFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	content := `
Host prod-api
  User deploy
  HostName 10.0.0.1
  Port 2222
  ProxyJump bastion
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{
		SSHConfig: path,
		Policy:    policy.Default(),
		Timeout:   2 * time.Second,
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	env := Doctor(cfg, "prod-api")
	if !env.OK {
		t.Fatalf("expected ok (probe may fail), got error %+v", env.Error)
	}
	if env.Action != "doctor" {
		t.Fatalf("action=%q, want doctor", env.Action)
	}

	var result struct {
		HostFound  bool   `json:"host_found"`
		User       string `json:"user"`
		Hostname   string `json:"hostname"`
		Port       string `json:"port"`
		ProxyJump  string `json:"proxy_jump"`
		PolicyOK   bool   `json:"policy_ok"`
		MuxEnabled bool   `json:"mux_enabled"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if !result.HostFound {
		t.Fatal("host_found=false, want true")
	}
	if result.User != "deploy" {
		t.Fatalf("user=%q, want deploy", result.User)
	}
	if result.Hostname != "10.0.0.1" {
		t.Fatalf("hostname=%q, want 10.0.0.1", result.Hostname)
	}
	if result.Port != "2222" {
		t.Fatalf("port=%q, want 2222", result.Port)
	}
	if result.ProxyJump != "bastion" {
		t.Fatalf("proxy_jump=%q, want bastion", result.ProxyJump)
	}
	if !result.PolicyOK {
		t.Fatal("policy_ok=false, want true")
	}
	if !result.MuxEnabled {
		t.Fatal("mux_enabled=false, want true (writable cache)")
	}
}

func TestDoctorMuxEnabledFalseWhenCacheUnwritable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte("Host prod-api\n  User deploy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CACHE_HOME", blocked)

	old := doctorRunner.SSHExec
	doctorRunner.SSHExec = func(ctx context.Context, opt runner.Options, host, cmd string) (*runner.ExecResult, error) {
		return &runner.ExecResult{ExitCode: 0}, nil
	}
	t.Cleanup(func() { doctorRunner.SSHExec = old })

	cfg := &Config{
		SSHConfig: path,
		Policy:    policy.Default(),
		Timeout:   2 * time.Second,
	}
	env := Doctor(cfg, "prod-api")
	if !env.OK {
		t.Fatalf("expected ok, got %+v", env.Error)
	}
	var result struct {
		MuxEnabled bool `json:"mux_enabled"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result.MuxEnabled {
		t.Fatal("mux_enabled=true, want false when cache is not writable")
	}
}

func TestDoctorUnknownHostNoProbe(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte("Host other\n  User u\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	probeCalled := false
	old := doctorRunner.SSHExec
	doctorRunner.SSHExec = func(ctx context.Context, opt runner.Options, host, cmd string) (*runner.ExecResult, error) {
		probeCalled = true
		return old(ctx, opt, host, cmd)
	}
	t.Cleanup(func() { doctorRunner.SSHExec = old })

	cfg := &Config{
		SSHConfig: path,
		Policy:    policy.Default(),
		Timeout:   2 * time.Second,
	}
	env := Doctor(cfg, "no-such-host-xyz")
	if env.OK {
		t.Fatal("expected failure for unknown host")
	}
	if env.Error == nil || env.Error.Code != response.CodeUsage {
		t.Fatalf("want usage, got %+v", env.Error)
	}
	if probeCalled {
		t.Fatal("SSHExec should not run when host not in config")
	}

	var result struct {
		HostFound bool `json:"host_found"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.HostFound {
		t.Fatal("host_found=true, want false")
	}
}

func TestDoctorEmptyHostUsage(t *testing.T) {
	cfg := &Config{Policy: policy.Default(), Timeout: 2 * time.Second}
	env := Doctor(cfg, "")
	if env.OK {
		t.Fatal("expected failure")
	}
	if env.Error == nil || env.Error.Code != response.CodeUsage {
		t.Fatalf("want usage, got %+v", env.Error)
	}
}
