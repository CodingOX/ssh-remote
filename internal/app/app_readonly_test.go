package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CodingOX/ssh-remote/internal/policy"
	"github.com/CodingOX/ssh-remote/internal/response"
	"github.com/CodingOX/ssh-remote/internal/runner"
)

// read_only=true 时 Put 在策略层即拒，不得发起 scp。
func TestPutDeniesWhenReadOnly(t *testing.T) {
	dir := t.TempDir()
	local := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(local, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}

	var scpCalled bool
	restore := stubPutRunner(t, putRunnerStubs{
		scpPut: func(ctx context.Context, opt runner.Options, host, localPath, remotePath string) (*runner.ExecResult, error) {
			scpCalled = true
			return &runner.ExecResult{ExitCode: 0}, nil
		},
	})
	defer restore()

	p := policy.Default()
	p.ReadOnly = true
	cfg := &Config{
		Policy:  p,
		Timeout: 2 * time.Second,
	}
	env := Put(cfg, "no-such-host-xyz", local, "/tmp/x.txt")
	if env.OK {
		t.Fatal("expected failure")
	}
	if env.Error == nil || env.Error.Code != response.CodePolicyDenied {
		t.Fatalf("want policy_denied, got %+v", env.Error)
	}
	if scpCalled {
		t.Fatal("must not scp when read_only=true")
	}
}

// read_only=true 时 Exec 非只读命令在策略层即拒，不得发起 ssh。
func TestExecDeniesNonReadOnlyCommandWhenReadOnly(t *testing.T) {
	p := policy.Default()
	p.ReadOnly = true
	cfg := &Config{
		Policy:  p,
		Timeout: 2 * time.Second,
	}
	env := Exec(cfg, "no-such-host-xyz", []string{"systemctl", "restart", "nginx"})
	if env.OK {
		t.Fatal("expected failure")
	}
	if env.Error == nil || env.Error.Code != response.CodePolicyDenied {
		t.Fatalf("want policy_denied, got %+v", env.Error)
	}
}

// read_only=true 时 Exec 只读命令通过策略检查，会走到 ssh（假 host 表现为 connect）。
func TestExecAllowsReadOnlyCommandWhenReadOnly(t *testing.T) {
	p := policy.Default()
	p.ReadOnly = true
	cfg := &Config{
		Policy:  p,
		Timeout: 2 * time.Second,
	}
	env := Exec(cfg, "no-such-host-xyz", []string{"df", "-h"})
	if env.OK {
		t.Fatal("expected failure (connect to fake host)")
	}
	if env.Error == nil {
		t.Fatal("expected error envelope")
	}
	if env.Error.Code == response.CodePolicyDenied {
		t.Fatalf("read-only allowed command must not be policy_denied, got %+v", env.Error)
	}
	if env.Error.Code != response.CodeConnect {
		t.Fatalf("want connect after read-only check passes, got %+v", env.Error)
	}
}
