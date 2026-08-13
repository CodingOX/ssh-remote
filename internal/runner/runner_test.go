package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestShellQuote(t *testing.T) {
	if got := ShellQuote("a'b"); got != `'"'"'a'"'"'b'"'"'` && got != `'a'"'"'b'` {
		// 标准形式：'a'"'"'b'
		if got != `'a'"'"'b'` {
			t.Fatalf("got %q", got)
		}
	}
}

func TestWrapWorkdir(t *testing.T) {
	got := WrapWorkdir("/var/log", "tail -n 20 app.log")
	want := "cd -- '/var/log' && tail -n 20 app.log"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if WrapWorkdir("", "true") != "true" {
		t.Fatal("empty workdir")
	}
}

func TestSSHBaseArgsIncludesConnectTimeout(t *testing.T) {
	args := sshBaseArgs(Options{})
	if !argsContainPair(args, "-o", "ConnectTimeout=10") {
		t.Fatalf("sshBaseArgs missing ConnectTimeout=10: %v", args)
	}
}

func TestSCPBaseArgsIncludesConnectTimeout(t *testing.T) {
	args := scpBaseArgs(Options{})
	if !argsContainPair(args, "-o", "ConnectTimeout=10") {
		t.Fatalf("scpBaseArgs missing ConnectTimeout=10: %v", args)
	}
}

func TestSSHBaseArgsIncludesControlMux(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)

	args := sshBaseArgs(Options{})
	if !argsContainPair(args, "-o", "ControlMaster=auto") {
		t.Fatalf("sshBaseArgs missing ControlMaster=auto: %v", args)
	}
	if !argsContainPair(args, "-o", "ControlPersist=60s") {
		t.Fatalf("sshBaseArgs missing ControlPersist=60s: %v", args)
	}
	wantPath := filepath.Join(cache, "ssh-remote", "cm-%C")
	if !argsContainPair(args, "-o", "ControlPath="+wantPath) {
		t.Fatalf("sshBaseArgs missing ControlPath=%q: %v", wantPath, args)
	}
}

func TestSCPBaseArgsIncludesControlMux(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)

	args := scpBaseArgs(Options{})
	if !argsContainPair(args, "-o", "ControlMaster=auto") {
		t.Fatalf("scpBaseArgs missing ControlMaster=auto: %v", args)
	}
	if !argsContainPair(args, "-o", "ControlPersist=60s") {
		t.Fatalf("scpBaseArgs missing ControlPersist=60s: %v", args)
	}
	wantPath := filepath.Join(cache, "ssh-remote", "cm-%C")
	if !argsContainPair(args, "-o", "ControlPath="+wantPath) {
		t.Fatalf("scpBaseArgs missing ControlPath=%q: %v", wantPath, args)
	}
}

func TestSSHBaseArgsDisableMuxExcludesControlMaster(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)

	args := sshBaseArgs(Options{DisableMux: true})
	if argsContainOptionPrefix(args, "ControlMaster=") {
		t.Fatalf("sshBaseArgs with DisableMux should omit ControlMaster: %v", args)
	}
	if argsContainOptionPrefix(args, "ControlPersist=") {
		t.Fatalf("sshBaseArgs with DisableMux should omit ControlPersist: %v", args)
	}
	if argsContainOptionPrefix(args, "ControlPath=") {
		t.Fatalf("sshBaseArgs with DisableMux should omit ControlPath: %v", args)
	}
	if MuxEnabled(Options{DisableMux: true}) {
		t.Fatal("MuxEnabled with DisableMux should be false")
	}
}

func TestMuxEnabledFalseWhenCacheNotWritable(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CACHE_HOME", blocked)
	if MuxEnabled(Options{}) {
		t.Fatal("MuxEnabled should be false when cache base is not a directory")
	}
}

func TestMuxEnabledTrueWhenCacheWritable(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if !MuxEnabled(Options{}) {
		t.Fatal("MuxEnabled should be true when cache dir is writable")
	}
}

func TestControlPathDirCreatedWith0700(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)

	_ = sshBaseArgs(Options{})

	dir := filepath.Join(cache, "ssh-remote")
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("control path dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("control path dir is not a directory")
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("control path dir mode=%o want 0700", info.Mode().Perm())
	}
}

func argsContainPair(args []string, key, val string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == key && args[i+1] == val {
			return true
		}
	}
	return false
}

func argsContainOptionPrefix(args []string, prefix string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-o" && strings.HasPrefix(args[i+1], prefix) {
			return true
		}
	}
	return false
}

// TestRunCapturedKillsProcessGroupOnTimeout 验证命令超时时整棵 ssh 进程树被清理（Setpgid + 杀进程组）。
func TestRunCapturedKillsProcessGroupOnTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process group kill is Unix-only")
	}

	dir := t.TempDir()
	childPIDFile := filepath.Join(dir, "child.pid")
	fakeSSH := filepath.Join(dir, "ssh")
	mainGo := filepath.Join(dir, "main.go")
	const fakeSSHMain = `package main

import (
	"os"
	"os/exec"
	"strconv"
)

func main() {
	pidFile := os.Getenv("RUNNER_TEST_CHILD_PIDFILE")
	child := exec.Command("sleep", "600")
	if err := child.Start(); err != nil {
		os.Exit(1)
	}
	_ = os.WriteFile(pidFile, []byte(strconv.Itoa(child.Process.Pid)), 0o644)
	_ = child.Wait()
}
`
	if err := os.WriteFile(mainGo, []byte(fakeSSHMain), 0o644); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", fakeSSH, mainGo)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake ssh: %v\n%s", err, out)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("RUNNER_TEST_CHILD_PIDFILE", childPIDFile)

	type runOutcome struct {
		res *ExecResult
		err error
	}
	done := make(chan runOutcome, 1)
	go func() {
		res, err := runCaptured(context.Background(), Options{Timeout: 2 * time.Second}, "ssh", []string{"host"})
		done <- runOutcome{res: res, err: err}
	}()

	// 先等到 fake ssh 写出子进程 PID，避免超时杀进程与写文件竞态。
	const pidPollInterval = 50 * time.Millisecond
	const pidPollMax = 5 * time.Second
	var (
		raw    []byte
		pidErr error
	)
	pidDeadline := time.Now().Add(pidPollMax)
	for time.Now().Before(pidDeadline) {
		raw, pidErr = os.ReadFile(childPIDFile)
		if pidErr == nil {
			break
		}
		time.Sleep(pidPollInterval)
	}

	outcome := <-done
	if outcome.err != nil {
		t.Fatalf("runCaptured: %v", outcome.err)
	}

	if pidErr != nil {
		sshPath, lookErr := exec.LookPath("ssh")
		t.Fatalf(
			"child pid file never appeared within %s: %v\nPATH=%q\nLookPath(ssh)=%q lookErr=%v\nfakeSSH=%q %s\nrunCaptured: TimedOut=%v ExitCode=%d stderr=%q",
			pidPollMax, pidErr,
			os.Getenv("PATH"),
			sshPath, lookErr,
			fakeSSH, fileExecSummary(fakeSSH),
			outcome.res.TimedOut, outcome.res.ExitCode, outcome.res.Stderr,
		)
	}

	if !outcome.res.TimedOut {
		t.Fatalf("expected TimedOut, got ExitCode=%d stderr=%q", outcome.res.ExitCode, outcome.res.Stderr)
	}

	childPID, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("parse child pid %q: %v", raw, err)
	}
	if processAlive(childPID) {
		t.Fatalf("child process %d still alive after timeout kill", childPID)
	}
}

func fileExecSummary(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Sprintf("stat err=%v", err)
	}
	return fmt.Sprintf("mode=%s size=%d executable=%v", info.Mode(), info.Size(), info.Mode()&0o111 != 0)
}

func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func TestLimitedBufferTruncates(t *testing.T) {
	var b limitedBuffer
	b.limit = 5
	_, _ = b.Write([]byte("hello world"))
	if b.String() != "hello" {
		t.Fatalf("buf=%q", b.String())
	}
	if !b.truncated {
		t.Fatal("expected truncated")
	}
}
