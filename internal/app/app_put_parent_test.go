package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CodingOX/ssh-remote/internal/policy"
	"github.com/CodingOX/ssh-remote/internal/response"
	"github.com/CodingOX/ssh-remote/internal/runner"
)

type putRunnerStubs struct {
	remoteHome func(context.Context, runner.Options, string) (string, error)
	sshExec    func(context.Context, runner.Options, string, string) (*runner.ExecResult, error)
	scpPut     func(context.Context, runner.Options, string, string, string) (*runner.ExecResult, error)
}

func stubPutRunner(t *testing.T, stubs putRunnerStubs) func() {
	t.Helper()
	prev := putRunner
	if stubs.remoteHome != nil {
		putRunner.RemoteHome = stubs.remoteHome
	}
	if stubs.sshExec != nil {
		putRunner.SSHExec = stubs.sshExec
	}
	if stubs.scpPut != nil {
		putRunner.SCPPut = stubs.scpPut
	}
	return func() {
		putRunner = prev
	}
}

func TestParentDirPOSIX(t *testing.T) {
	cases := []struct {
		remotePath string
		want       string
	}{
		{"/tmp/agent/fix.sh", "/tmp/agent"},
		{"~/agent-drop/fix.sh", "~/agent-drop"},
		{"/tmp/fix.sh", "/tmp"},
		{"/fix.sh", "/"},
	}
	for _, tc := range cases {
		t.Run(tc.remotePath, func(t *testing.T) {
			if got := parentDirPOSIX(tc.remotePath); got != tc.want {
				t.Fatalf("parentDirPOSIX(%q) = %q, want %q", tc.remotePath, got, tc.want)
			}
		})
	}
}

// 精确文件白名单时，目标文件允许但父目录不在名单 → policy_denied，且不 ssh mkdir。
func TestPutDeniesMkdirWhenParentNotInAllowlist(t *testing.T) {
	dir := t.TempDir()
	local := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(local, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}

	var mkdirCalled bool
	restore := stubPutRunner(t, putRunnerStubs{
		sshExec: func(ctx context.Context, opt runner.Options, host, command string) (*runner.ExecResult, error) {
			if strings.HasPrefix(command, "mkdir -p -- ") {
				mkdirCalled = true
			}
			return &runner.ExecResult{ExitCode: 0}, nil
		},
	})
	defer restore()

	cfg := &Config{
		Policy: &policy.Policy{
			CommandTimeoutMs: 60_000,
			MaxOutputBytes:   1 << 20,
			MaxFileBytes:     5 << 20,
			WriteAllowlist:   []string{"/tmp/agent/file.sh"},
		},
		Timeout: 2 * time.Second,
	}
	env := Put(cfg, "host", local, "/tmp/agent/file.sh")
	if env.OK {
		t.Fatal("expected failure")
	}
	if env.Error == nil || env.Error.Code != response.CodePolicyDenied {
		t.Fatalf("want policy_denied, got %+v", env.Error)
	}
	if mkdirCalled {
		t.Fatal("must not ssh mkdir when parent dir is not in write allowlist")
	}
}

func TestPutMkdirsParentBeforeSCP(t *testing.T) {
	dir := t.TempDir()
	local := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(local, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}

	var mkdirCmd string
	var scpCalled bool
	restore := stubPutRunner(t, putRunnerStubs{
		sshExec: func(ctx context.Context, opt runner.Options, host, command string) (*runner.ExecResult, error) {
			if strings.HasPrefix(command, "mkdir -p -- ") {
				mkdirCmd = command
			}
			return &runner.ExecResult{ExitCode: 0}, nil
		},
		scpPut: func(ctx context.Context, opt runner.Options, host, localPath, remotePath string) (*runner.ExecResult, error) {
			scpCalled = true
			return &runner.ExecResult{ExitCode: 0}, nil
		},
	})
	defer restore()

	cfg := &Config{
		Policy:  policy.Default(),
		Timeout: 2 * time.Second,
	}
	env := Put(cfg, "host", local, "/tmp/agent/fix.sh")
	if !env.OK {
		t.Fatalf("expected success, got %+v", env.Error)
	}
	wantMkdir := "mkdir -p -- " + runner.ShellQuote("/tmp/agent")
	if mkdirCmd != wantMkdir {
		t.Fatalf("mkdir command = %q, want %q", mkdirCmd, wantMkdir)
	}
	if !scpCalled {
		t.Fatal("scp should run after mkdir")
	}
}

func TestPutMkdirFailureUsesRemoteFS(t *testing.T) {
	dir := t.TempDir()
	local := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(local, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}

	restore := stubPutRunner(t, putRunnerStubs{
		sshExec: func(ctx context.Context, opt runner.Options, host, command string) (*runner.ExecResult, error) {
			if strings.HasPrefix(command, "mkdir -p -- ") {
				return &runner.ExecResult{
					ExitCode: 1,
					Stderr:   "mkdir: cannot create directory '/tmp/agent': Permission denied",
				}, nil
			}
			return &runner.ExecResult{ExitCode: 0}, nil
		},
		scpPut: func(ctx context.Context, opt runner.Options, host, localPath, remotePath string) (*runner.ExecResult, error) {
			t.Fatal("scp must not run when mkdir fails")
			return nil, nil
		},
	})
	defer restore()

	cfg := &Config{
		Policy:  policy.Default(),
		Timeout: 2 * time.Second,
	}
	env := Put(cfg, "host", local, "/tmp/agent/fix.sh")
	if env.OK {
		t.Fatal("expected failure")
	}
	if env.Error == nil || env.Error.Code != response.CodeRemoteFS {
		t.Fatalf("want remote_fs, got %+v", env.Error)
	}
}

func TestPutSkipsMkdirWhenParentIsRoot(t *testing.T) {
	dir := t.TempDir()
	local := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(local, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}

	var mkdirCalled bool
	restore := stubPutRunner(t, putRunnerStubs{
		sshExec: func(ctx context.Context, opt runner.Options, host, command string) (*runner.ExecResult, error) {
			if strings.HasPrefix(command, "mkdir -p -- ") {
				mkdirCalled = true
			}
			return &runner.ExecResult{ExitCode: 0}, nil
		},
		scpPut: func(ctx context.Context, opt runner.Options, host, localPath, remotePath string) (*runner.ExecResult, error) {
			return &runner.ExecResult{ExitCode: 0}, nil
		},
	})
	defer restore()

	cfg := &Config{
		Policy: &policy.Policy{
			CommandTimeoutMs: 60_000,
			MaxOutputBytes:   1 << 20,
			MaxFileBytes:     5 << 20,
			WriteAllowlist:   []string{"/fix.sh"},
		},
		Timeout: 2 * time.Second,
	}
	env := Put(cfg, "host", local, "/fix.sh")
	if !env.OK {
		t.Fatalf("expected success, got %+v", env.Error)
	}
	if mkdirCalled {
		t.Fatal("must not mkdir when parent is /")
	}
}
