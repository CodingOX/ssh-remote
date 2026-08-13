package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CodingOX/ssh-remote/internal/policy"
	"github.com/CodingOX/ssh-remote/internal/response"
)

// Put 到绝对禁止路径时，不得依赖网络（本地策略即拒）。
func TestPutDeniesEtcWithoutNetwork(t *testing.T) {
	dir := t.TempDir()
	local := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(local, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{
		Policy:  policy.Default(),
		Timeout: 2 * time.Second,
	}
	env := Put(cfg, "no-such-host-xyz", local, "/etc/nginx/nginx.conf")
	if env.OK {
		t.Fatal("expected failure")
	}
	if env.Error == nil || env.Error.Code != response.CodePolicyDenied {
		t.Fatalf("want policy_denied, got %+v", env.Error)
	}
}

// Get ~/.ssh/ 远端路径时即使未解析 remoteHome 也应在策略层拒绝，不得发起 ssh。
func TestGetDeniesRemoteSSHKeyWithoutRemoteHome(t *testing.T) {
	cfg := &Config{
		Policy:  policy.Default(),
		Timeout: 2 * time.Second,
	}
	env := Get(cfg, "no-such-host-xyz", "~/.ssh/id_ed25519", "")
	if env.OK {
		t.Fatal("expected failure")
	}
	if env.Error == nil || env.Error.Code != response.CodePolicyDenied {
		t.Fatalf("want policy_denied, got %+v", env.Error)
	}
}

// Get 本机目标落在 ~/.ssh/ 或含 /.git/ 时策略层即拒，不得发起 ssh。
func TestGetDeniesLocalDestSSHAndGitWithoutNetwork(t *testing.T) {
	cfg := &Config{
		Policy:  policy.Default(),
		Timeout: 2 * time.Second,
	}

	t.Run("ssh dest", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
			t.Fatal(err)
		}
		local := filepath.Join(home, ".ssh", "downloaded")
		env := Get(cfg, "no-such-host-xyz", "/tmp/allowed.txt", local)
		if env.OK {
			t.Fatal("expected failure")
		}
		if env.Error == nil || env.Error.Code != response.CodePolicyDenied {
			t.Fatalf("want policy_denied, got %+v", env.Error)
		}
	})

	t.Run("git dest", func(t *testing.T) {
		repo := filepath.Join(t.TempDir(), "repo")
		if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
			t.Fatal(err)
		}
		local := filepath.Join(repo, ".git", "config")
		env := Get(cfg, "no-such-host-xyz", "/tmp/allowed.txt", local)
		if env.OK {
			t.Fatal("expected failure")
		}
		if env.Error == nil || env.Error.Code != response.CodePolicyDenied {
			t.Fatalf("want policy_denied, got %+v", env.Error)
		}
	})

	t.Run("nested git segment", func(t *testing.T) {
		work := filepath.Join(t.TempDir(), "work")
		if err := os.MkdirAll(filepath.Join(work, ".git"), 0o700); err != nil {
			t.Fatal(err)
		}
		local := filepath.Join(work, ".git", "HEAD")
		env := Get(cfg, "no-such-host-xyz", "/tmp/allowed.txt", local)
		if env.OK {
			t.Fatal("expected failure")
		}
		if env.Error == nil || env.Error.Code != response.CodePolicyDenied {
			t.Fatalf("want policy_denied, got %+v", env.Error)
		}
	})
}

// Put 本机 ~/.ssh/ 源文件时策略层即拒，不得发起 ssh。
func TestPutDeniesLocalSSHKeySourceWithoutNetwork(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(home, ".ssh", "id_ed25519")
	if err := os.WriteFile(local, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{
		Policy:  policy.Default(),
		Timeout: 2 * time.Second,
	}
	env := Put(cfg, "no-such-host-xyz", local, "/tmp/x.txt")
	if env.OK {
		t.Fatal("expected failure")
	}
	if env.Error == nil || env.Error.Code != response.CodePolicyDenied {
		t.Fatalf("want policy_denied, got %+v", env.Error)
	}
}

// Get /etc/shadow 时策略层即拒，不得发起 ssh。
func TestGetDeniesEtcShadowWithoutNetwork(t *testing.T) {
	cfg := &Config{
		Policy:  policy.Default(),
		Timeout: 2 * time.Second,
	}
	env := Get(cfg, "no-such-host-xyz", "/etc/shadow", "")
	if env.OK {
		t.Fatal("expected failure")
	}
	if env.Error == nil || env.Error.Code != response.CodePolicyDenied {
		t.Fatalf("want policy_denied, got %+v", env.Error)
	}
}

// Get 绝对路径 /.ssh/ 时即使未解析 remoteHome 也应在策略层拒绝，不得发起 ssh。
func TestGetDeniesAbsoluteSSHPathWithoutNetwork(t *testing.T) {
	cfg := &Config{
		Policy:  policy.Default(),
		Timeout: 2 * time.Second,
	}
	env := Get(cfg, "no-such-host-xyz", "/home/deploy/.ssh/id_ed25519", "")
	if env.OK {
		t.Fatal("expected failure")
	}
	if env.Error == nil || env.Error.Code != response.CodePolicyDenied {
		t.Fatalf("want policy_denied, got %+v", env.Error)
	}
}

func TestExecDeniesBlacklistWithoutNetwork(t *testing.T) {
	cfg := &Config{
		Policy:  policy.Default(),
		Timeout: 2 * time.Second,
	}
	env := Exec(cfg, "no-such-host-xyz", []string{"rm", "-rf", "/"})
	if env.OK {
		t.Fatal("expected failure")
	}
	if env.Error == nil || env.Error.Code != response.CodePolicyDenied {
		t.Fatalf("want policy_denied, got %+v", env.Error)
	}
}
