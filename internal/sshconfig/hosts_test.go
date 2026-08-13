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

func TestListHostEntriesSkipsPatterns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	content := `
Host *
  User root
Host prod-api bastion
  User deploy
Host dev?
  User dev
Host staging-web
  HostName 10.0.0.1
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := ListHostEntries(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]HostEntry{
		"prod-api": {
			Host: "prod-api", User: "deploy",
		},
		"bastion": {
			Host: "bastion", User: "deploy",
		},
		"staging-web": {
			Host: "staging-web", HostName: "10.0.0.1",
		},
	}
	if len(entries) != len(want) {
		t.Fatalf("entries=%v", entries)
	}
	for _, e := range entries {
		w, ok := want[e.Host]
		if !ok {
			t.Fatalf("unexpected host %s", e.Host)
		}
		if e != w {
			t.Fatalf("host %s: got %+v, want %+v", e.Host, e, w)
		}
	}
}

func TestListHostEntriesInclude(t *testing.T) {
	dir := t.TempDir()
	included := filepath.Join(dir, "included")
	if err := os.WriteFile(included, []byte(`
Host from-include
  User inc
  HostName inc.example.com
`), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config")
	content := `
Include included
Host main
  User main
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := ListHostEntries(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]HostEntry{
		"main": {
			Host: "main", User: "main",
		},
		"from-include": {
			Host: "from-include", User: "inc", HostName: "inc.example.com",
		},
	}
	if len(entries) != len(want) {
		t.Fatalf("entries=%v", entries)
	}
	for _, e := range entries {
		w, ok := want[e.Host]
		if !ok {
			t.Fatalf("unexpected host %s", e.Host)
		}
		if e != w {
			t.Fatalf("host %s: got %+v, want %+v", e.Host, e, w)
		}
	}
}

func TestListHostEntriesLaterKeywordOverridesInBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	content := `
Host prod-api
  User first
  User second
  Port 22
  Port 2222
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := ListHostEntries(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries=%v", entries)
	}
	want := HostEntry{Host: "prod-api", User: "second", Port: "2222"}
	if entries[0] != want {
		t.Fatalf("got %+v, want %+v", entries[0], want)
	}
}

func TestListHostEntriesFirstBlockWinsForDuplicateAlias(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	content := `
Host prod-api
  User first
  HostName first.example.com
Host prod-api
  User second
  HostName second.example.com
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := ListHostEntries(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries=%v", entries)
	}
	want := HostEntry{Host: "prod-api", User: "first", HostName: "first.example.com"}
	if entries[0] != want {
		t.Fatalf("got %+v, want %+v", entries[0], want)
	}
}

func TestListHostEntriesMissingFile(t *testing.T) {
	entries, err := ListHostEntries(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("want empty, got %v", entries)
	}
}

func TestListHostEntriesProdAPI(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	content := `
Host prod-api
  User deploy
  HostName api.example.com
  Port 2222
  ProxyJump bastion
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := ListHostEntries(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries=%v", entries)
	}
	got := entries[0]
	want := HostEntry{
		Host:      "prod-api",
		User:      "deploy",
		HostName:  "api.example.com",
		Port:      "2222",
		ProxyJump: "bastion",
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
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
