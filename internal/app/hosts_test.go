package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CodingOX/ssh-remote/internal/policy"
)

// Hosts 应返回带寻址字段的条目对象，不探测网络。
func TestHostsReturnsEntryObjectsWithAddressFields(t *testing.T) {
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
	env := Hosts(cfg)
	if !env.OK {
		t.Fatalf("expected ok, got error %+v", env.Error)
	}

	var result struct {
		Hosts []struct {
			Name      string `json:"name"`
			Hostname  string `json:"hostname"`
			User      string `json:"user"`
			Port      string `json:"port"`
			ProxyJump string `json:"proxy_jump"`
		} `json:"hosts"`
		ConfigPath string `json:"config_path"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.ConfigPath != path {
		t.Fatalf("config_path=%q, want %q", result.ConfigPath, path)
	}
	if len(result.Hosts) != 1 {
		t.Fatalf("hosts len=%d, want 1", len(result.Hosts))
	}
	got := result.Hosts[0]
	want := struct {
		Name      string
		Hostname  string
		User      string
		Port      string
		ProxyJump string
	}{
		Name:      "prod-api",
		Hostname:  "10.0.0.1",
		User:      "deploy",
		Port:      "2222",
		ProxyJump: "bastion",
	}
	if got.Name != want.Name || got.Hostname != want.Hostname ||
		got.User != want.User || got.Port != want.Port || got.ProxyJump != want.ProxyJump {
		t.Fatalf("host entry=%+v, want name=%q hostname=%q user=%q port=%q proxy_jump=%q",
			got, want.Name, want.Hostname, want.User, want.Port, want.ProxyJump)
	}
}
