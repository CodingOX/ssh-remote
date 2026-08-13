package app

import (
	"testing"
	"time"

	"github.com/CodingOX/ssh-remote/internal/response"
)

func TestClassifyTransferErrorRemoteFS(t *testing.T) {
	cases := []struct {
		name   string
		stderr string
	}{
		{"permission denied", "scp: /tmp/x: Permission denied"},
		{"no such file", "scp: /tmp/x: No such file or directory"},
		{"not regular file", "scp: /tmp/x: not a regular file"},
		{"is directory", "scp: /tmp/x: Is a directory"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyTransferError(1, tc.stderr); got != response.CodeRemoteFS {
				t.Fatalf("classifyTransferError(1, %q) = %q, want %q", tc.stderr, got, response.CodeRemoteFS)
			}
		})
	}
}

func TestClassifyTransferErrorConnect(t *testing.T) {
	cases := []struct {
		name   string
		stderr string
	}{
		{"connection refused", "ssh: connect to host 10.0.0.1 port 22: Connection refused"},
		{"could not resolve", "ssh: Could not resolve hostname bad.example: nodename nor servname provided"},
		{"publickey", "deploy@host: Permission denied (publickey,password)."},
		{"try again", "Permission denied, please try again."},
		{"bare permission denied", "Permission denied"},
		{"host key", "Host key verification failed."},
		{"unreachable", "ssh: connect to host 10.0.0.1 port 22: Network is unreachable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyTransferError(1, tc.stderr); got != response.CodeConnect {
				t.Fatalf("classifyTransferError(1, %q) = %q, want %q", tc.stderr, got, response.CodeConnect)
			}
		})
	}
}

func TestClassifyTransferErrorUnknownDefaultsConnect(t *testing.T) {
	if got := classifyTransferError(1, "scp: some unexpected error"); got != response.CodeConnect {
		t.Fatalf("unknown stderr should default to connect, got %q", got)
	}
}

func TestFailAndOkEnvSetRetriable(t *testing.T) {
	connectFail := fail("get", "host", response.CodeConnect, "dial failed", time.Second, nil)
	if !connectFail.Retriable {
		t.Fatal("connect failure envelope should be retriable")
	}
	remoteFSFail := fail("get", "host", response.CodeRemoteFS, "permission denied", time.Second, nil)
	if remoteFSFail.Retriable {
		t.Fatal("remote_fs failure envelope should not be retriable")
	}
	timeoutFail := fail("get", "host", response.CodeTimeout, "timed out", time.Second, nil)
	if !timeoutFail.Retriable {
		t.Fatal("timeout failure envelope should be retriable")
	}
	ok := okEnv("get", "host", time.Second, false, nil)
	if ok.Retriable {
		t.Fatal("ok envelope should have retriable=false")
	}
}
