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
		CodeRemoteFS:     ExitRemoteFS,
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

func TestWriteIncludesRetriable(t *testing.T) {
	t.Run("failure explicit false", func(t *testing.T) {
		var buf bytes.Buffer
		env := Envelope{
			OK:        false,
			Action:    "put",
			Retriable: false,
			Error:     &ErrorBody{Code: CodePolicyDenied, Message: "denied"},
			Meta: Meta{
				Version:   "0.1.0",
				TimeoutMs: 60000,
			},
		}
		if err := Write(&buf, env); err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		retriable, ok := m["retriable"].(bool)
		if !ok {
			t.Fatalf("retriable missing or not bool: %v", m)
		}
		if retriable {
			t.Fatal("want retriable=false")
		}
	})

	t.Run("success explicit false", func(t *testing.T) {
		var buf bytes.Buffer
		env := Envelope{
			OK:        true,
			Action:    "put",
			Retriable: false,
			Meta: Meta{
				Version:   "0.1.0",
				TimeoutMs: 60000,
			},
			Result: json.RawMessage(`{}`),
		}
		if err := Write(&buf, env); err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		if m["error"] != nil {
			t.Fatalf("error should be null: %v", m["error"])
		}
		retriable, ok := m["retriable"].(bool)
		if !ok {
			t.Fatalf("retriable missing or not bool: %v", m)
		}
		if retriable {
			t.Fatal("want retriable=false on success")
		}
	})

	t.Run("connect retriable true", func(t *testing.T) {
		var buf bytes.Buffer
		env := Envelope{
			OK:        false,
			Action:    "exec",
			Retriable: true,
			Error:     &ErrorBody{Code: CodeConnect, Message: "dial failed"},
			Meta: Meta{
				Version:   "0.1.0",
				TimeoutMs: 60000,
			},
		}
		if err := Write(&buf, env); err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		if m["retriable"] != true {
			t.Fatalf("want retriable=true, got %v", m["retriable"])
		}
	})
}

func TestRetriableFor(t *testing.T) {
	cases := []struct {
		ok   bool
		code string
		want bool
	}{
		{ok: true, code: "", want: false},
		{ok: false, code: CodeConnect, want: true},
		{ok: false, code: CodeTimeout, want: true},
		{ok: false, code: CodeUsage, want: false},
		{ok: false, code: CodePolicyDenied, want: false},
		{ok: false, code: CodeLocalFS, want: false},
		{ok: false, code: CodeRemoteFS, want: false},
		{ok: false, code: CodeRemoteExit, want: false},
		{ok: false, code: CodeInternal, want: false},
	}
	for _, tc := range cases {
		name := tc.code
		if tc.ok {
			name = "ok"
		}
		t.Run(name, func(t *testing.T) {
			if got := RetriableFor(tc.code, tc.ok); got != tc.want {
				t.Fatalf("RetriableFor(%q, %v): got %v want %v", tc.code, tc.ok, got, tc.want)
			}
		})
	}
}
