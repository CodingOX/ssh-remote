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
