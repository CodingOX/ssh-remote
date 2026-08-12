package sshconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListHostsSkipsPatterns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	content := `
Host *
  Compression yes
Host prod-api bastion
  User deploy
Host dev?
  User dev
# Host commented
Host staging-web
  HostName 10.0.0.1
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	hosts, err := ListHosts(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"prod-api": true, "bastion": true, "staging-web": true}
	if len(hosts) != 3 {
		t.Fatalf("hosts=%v", hosts)
	}
	for _, h := range hosts {
		if !want[h] {
			t.Fatalf("unexpected host %s in %v", h, hosts)
		}
	}
}

func TestListHostsMissingFile(t *testing.T) {
	hosts, err := ListHosts(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 0 {
		t.Fatalf("want empty, got %v", hosts)
	}
}
