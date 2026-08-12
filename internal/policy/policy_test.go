package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultDeniesDangerousCommands(t *testing.T) {
	p := Default()
	cases := []string{
		"rm -rf /",
		"rm -rf /*",
		"dd if=/dev/zero of=/dev/sda",
		"mkfs.ext4 /dev/sdb1",
		"wipefs -a /dev/sda",
	}
	for _, c := range cases {
		if err := p.CheckCommand(c); err == nil {
			t.Fatalf("expected deny for %q", c)
		}
	}
}

func TestDefaultAllowsReadCommands(t *testing.T) {
	p := Default()
	for _, c := range []string{"df -h", "ls -la /var/log", "systemctl status nginx"} {
		if err := p.CheckCommand(c); err != nil {
			t.Fatalf("expected allow for %q: %v", c, err)
		}
	}
}

func TestWriteAllowlistTmp(t *testing.T) {
	p := Default()
	if err := p.CheckWritePath("/tmp/fix.sh", "/home/deploy"); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckWritePath("/etc/nginx/nginx.conf", "/home/deploy"); err == nil {
		t.Fatal("expected deny /etc")
	}
}

func TestWriteAllowlistHomeDrop(t *testing.T) {
	p := Default()
	if err := p.CheckWritePath("~/agent-drop/fix.sh", "/home/deploy"); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckWritePath("/home/deploy/agent-drop/x.sh", "/home/deploy"); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckWritePath("~/secrets/key", "/home/deploy"); err == nil {
		t.Fatal("expected deny outside agent-drop")
	}
}

func TestWriteRejectDotDot(t *testing.T) {
	p := Default()
	if err := p.CheckWritePath("/tmp/../etc/passwd", "/home/deploy"); err == nil {
		t.Fatal("expected deny path with ..")
	}
}

func TestLoadUnknownKeyFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.toml")
	if err := os.WriteFile(path, []byte("not_a_real_key = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected unknown key error")
	}
}

func TestLoadOverridesTimeout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.toml")
	if err := os.WriteFile(path, []byte("command_timeout_ms = 12000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if p.CommandTimeoutMs != 12000 {
		t.Fatalf("timeout=%d", p.CommandTimeoutMs)
	}
	// denylist still default
	if err := p.CheckCommand("rm -rf /"); err == nil {
		t.Fatal("default denylist should remain")
	}
}

func TestLoadMissingFileUsesDefault(t *testing.T) {
	p, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if p.MaxFileBytes != 5_242_880 {
		t.Fatalf("unexpected default file bytes %d", p.MaxFileBytes)
	}
}
