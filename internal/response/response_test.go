package response

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestExitForCode(t *testing.T) {
	cases := map[string]int{
		"":               ExitOK,
		CodeUsage:        ExitUsage,
		CodeLocalFS:      ExitUsage,
		CodePolicyDenied: ExitPolicy,
		CodeRemoteExit:   ExitRemote,
		CodeConnect:      ExitConnect,
		CodeTimeout:      ExitConnect,
		CodeInternal:     ExitInternal,
		"other":          ExitInternal,
	}
	for code, want := range cases {
		if got := ExitForCode(code); got != want {
			t.Fatalf("code %q: got %d want %d", code, got, want)
		}
	}
}

func TestWriteJSONShape(t *testing.T) {
	var buf bytes.Buffer
	env := Envelope{
		OK:     false,
		Action: "put",
		Error:  &ErrorBody{Code: CodePolicyDenied, Message: "denied"},
		Meta: Meta{
			Version:   "0.1.0",
			Host:      HostPtr("prod-api"),
			TimeoutMs: 60000,
		},
		Result: nil,
	}
	if err := Write(&buf, env); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m["ok"] != false || m["action"] != "put" {
		t.Fatalf("shape: %v", m)
	}
	errObj := m["error"].(map[string]any)
	if errObj["code"] != CodePolicyDenied {
		t.Fatalf("error: %v", errObj)
	}
}
