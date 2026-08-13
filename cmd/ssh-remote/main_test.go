package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/CodingOX/ssh-remote/internal/response"
	"github.com/CodingOX/ssh-remote/internal/version"
)

func TestUsageEnvRetriableFalse(t *testing.T) {
	env := usageEnv("exec", "usage: exec <host> -- <command...>")

	if env.Retriable {
		t.Fatal("want Retriable=false on usage envelope")
	}

	var buf bytes.Buffer
	if err := response.Write(&buf, env); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m["retriable"] != false {
		t.Fatalf("json retriable: got %v want false", m["retriable"])
	}
}

func TestParseDoctorMissingHost(t *testing.T) {
	_, err := parseDoctor(nil)
	if err == nil {
		t.Fatal("want error when host missing")
	}
	if !strings.Contains(err.Error(), "doctor") {
		t.Fatalf("error: got %q want doctor usage hint", err.Error())
	}
}

func TestVersionEnv(t *testing.T) {
	env := versionEnv()

	if !env.OK {
		t.Fatalf("ok: got false want true")
	}
	if env.Action != "version" {
		t.Fatalf("action: got %q want version", env.Action)
	}
	if env.Error != nil {
		t.Fatalf("error: got %v want nil", env.Error)
	}
	if env.Meta.Version != version.Version {
		t.Fatalf("meta.version: got %q want %q", env.Meta.Version, version.Version)
	}

	var result map[string]string
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatal(err)
	}
	if got := result["version"]; got != version.Version {
		t.Fatalf("result.version: got %q want %q", got, version.Version)
	}

	var buf bytes.Buffer
	if err := response.Write(&buf, env); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m["ok"] != true || m["action"] != "version" {
		t.Fatalf("json shape: %v", m)
	}
	if m["error"] != nil {
		t.Fatalf("json error: got %v want null", m["error"])
	}
	if m["retriable"] != false {
		t.Fatalf("json retriable: got %v want false", m["retriable"])
	}
	if env.Retriable {
		t.Fatal("version envelope should have retriable=false")
	}
	meta := m["meta"].(map[string]any)
	if meta["version"] != version.Version {
		t.Fatalf("json meta.version: got %v want %q", meta["version"], version.Version)
	}
}

func TestParseGlobalVersionAndHelpFlags(t *testing.T) {
	_, args, err := parseGlobal([]string{"--version"})
	if err != nil {
		t.Fatalf("parseGlobal --version: %v", err)
	}
	if len(args) != 1 || args[0] != "version" {
		t.Fatalf("args=%v want [version]", args)
	}
	_, args, err = parseGlobal([]string{"-V"})
	if err != nil {
		t.Fatalf("parseGlobal -V: %v", err)
	}
	if len(args) != 1 || args[0] != "version" {
		t.Fatalf("args=%v want [version]", args)
	}
	if !isMetaSub("version") || !isMetaSub("help") {
		t.Fatal("isMetaSub should accept version and help")
	}
	if isMetaSub("exec") {
		t.Fatal("isMetaSub should not accept exec")
	}
}
