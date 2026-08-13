package audit

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// ── Redact 脱敏用例 ──────────────────────────────────────────────────────────

// 敏感形态都应被替换为 ***，且保留上下文（flag 名 / 参数名）以便追溯。
func TestRedactSensitiveForms(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"长选项赋值", `--password=hunter2`, `--password="***"`},
		{"长选项空格值", `--token abc123`, `--token "***"`},
		{"复合键赋值", `--access-key=AKIA123`, `--access-key="***"`},
		{"sshpass 短选项", `sshpass -p secret ssh host`, `sshpass -p "***" ssh host`},
		{"URL query 参数", `curl "https://x.com?token=abc&page=2"`, `curl "https://x.com?token=***&page=2"`},
		{"URL 后续 query token", `curl "https://x.com?page=2&token=abc"`, `curl "https://x.com?page=2&token=***"`},
		{"Authorization 头", `curl -H "Authorization: Bearer eyJhbGciOi" https://x.com`, `curl -H "Authorization: Bearer ***" https://x.com`},
		{"独立 Bearer 串", `echo Bearer abcdef123456`, `echo Bearer ***`},
		{"带前缀环境变量", `export AWS_SECRET_ACCESS_KEY=abc123`, `export AWS_SECRET_ACCESS_KEY="***"`},
		{"裸 TOKEN 环境变量", `TOKEN=secret cmd`, `TOKEN="***" cmd`},
		{"裸 PASSWORD 环境变量", `PASSWORD=hunter2 cmd`, `PASSWORD="***" cmd`},
		{"export TOKEN", `export TOKEN=abc`, `export TOKEN="***"`},
		{"中间环境变量", `FOO=1 TOKEN=secret BAR=2`, `FOO=1 TOKEN="***" BAR=2`},
		{"&& 后环境变量", `cmd && TOKEN=secret`, `cmd && TOKEN="***"`},
		{"引号包裹的值", `--password "hunter two"`, `--password "***"`},
	}
	for _, c := range cases {
		if got := Redact(c.in); got != c.want {
			t.Errorf("%s: Redact(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// 通用词与无害命令不应被误伤。
func TestRedactNoFalsePositive(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"普通命令", `ls -la /tmp`},
		{"短选项 -p 不脱敏", `ssh -p 2222 host`},
		{"长选项非敏感", `--port 8080 --verbose`},
		{"keyfile 路径不脱敏", `--keyfile /tmp/id_rsa`},
		{"ssh-keygen 不脱敏", `ssh-keygen -t rsa -b 4096`},
		{"pwd 命令不脱敏", `pwd && cd /var`},
		{"短 Bearer 值不脱敏", `echo Bearer abc`},
	}
	for _, c := range cases {
		if got := Redact(c.in); got != c.in {
			t.Errorf("%s: Redact(%q) = %q, want unchanged", c.name, c.in, got)
		}
	}
}

// 幂等性：重复脱敏结果不变（后续规则不二次改写 ***）。
func TestRedactIdempotent(t *testing.T) {
	in := `curl --token abc "https://x.com?page=2&token=def" TOKEN=secret -H "Authorization: Bearer xyz"`
	once := Redact(in)
	twice := Redact(once)
	if once != twice {
		t.Errorf("Redact not idempotent: %q -> %q", once, twice)
	}
	if !strings.Contains(once, `TOKEN="***"`) {
		t.Errorf("expected TOKEN redacted, got %q", once)
	}
	if !strings.Contains(once, `&token=***`) || strings.Contains(once, `&token="***"`) {
		t.Errorf("URL &token= should stay unquoted ***, got %q", once)
	}
}

// ── Append 写入与轮转用例 ────────────────────────────────────────────────────

// 基本追加：父目录自动创建，逐行 JSONL，文件权限收紧为 0600。
func TestAppendBasic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "audit.jsonl")
	rec := Record{TS: "2025-01-01T10:00:00+08:00", Action: "exec", Host: "h", Cmd: "ls", ExitCode: 0}
	if err := Append(path, rec, 1024, 3); err != nil {
		t.Fatal(err)
	}
	if err := Append(path, rec, 1024, 3); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %s", len(lines), data)
	}
	if !strings.Contains(lines[0], `"action":"exec"`) {
		t.Errorf("unexpected line: %s", lines[0])
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("file perm = %o, want 600", fi.Mode().Perm())
	}
}

// 超阈值轮转：当前文件超 maxSize 时整体后移为 .1，新记录写入新文件。
func TestAppendRotateOnSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	rec := Record{Action: "exec", Cmd: "ls"}
	// maxSize 足够小：第二条写入时触发轮转
	if err := Append(path, rec, 50, 3); err != nil {
		t.Fatal(err)
	}
	if err := Append(path, rec, 50, 3); err != nil {
		t.Fatal(err)
	}
	// 当前文件只有 1 条（r2），r1 已轮转为 .1
	cur, _ := os.ReadFile(path)
	if strings.Count(string(cur), "\n") != 1 {
		t.Errorf("current file want 1 line, got: %s", cur)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Errorf(".1 should exist after rotate: %v", err)
	}
}

// keep 上限：保留份数封顶，最老文件在下次轮转时被删除。
func TestAppendKeepLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	// 写入 4 条，maxSize 保证每次追加都触发轮转（单条约 35B > 30B）
	for i := 0; i < 4; i++ {
		rec := Record{Action: "exec", Cmd: "ls", ExitCode: i}
		if err := Append(path, rec, 30, 2); err != nil {
			t.Fatal(err)
		}
	}
	// 期望：当前=r4，.1=r3，.2=r2，r1 已被删除
	cur, _ := os.ReadFile(path)
	if !strings.Contains(string(cur), `"exit_code":3`) {
		t.Errorf("current file want r4, got: %s", cur)
	}
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Errorf(".3 should be deleted (keep=2), err=%v", err)
	}
	b1, _ := os.ReadFile(path + ".1")
	if !strings.Contains(string(b1), `"exit_code":2`) {
		t.Errorf(".1 want r3, got: %s", b1)
	}
	b2, _ := os.ReadFile(path + ".2")
	if !strings.Contains(string(b2), `"exit_code":1`) {
		t.Errorf(".2 want r2, got: %s", b2)
	}
}

// 轮转结果整体可恢复：合并当前文件 + 各份历史，全部记录无丢失、无乱序。
func TestAppendNoRecordLoss(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	// keep=10 足够容纳 7 条（每条单独一份），maxSize=30 保证每次追加都轮转
	for i := 0; i < 7; i++ {
		rec := Record{Action: "exec", Cmd: "ls", ExitCode: i}
		if err := Append(path, rec, 30, 10); err != nil {
			t.Fatal(err)
		}
	}
	// 按 .10 → … → .1 → 当前 顺序拼接，验证 7 条记录都还在且顺序正确
	var got []string
	for i := 10; i >= 1; i-- {
		if b, err := os.ReadFile(path + "." + strconv.Itoa(i)); err == nil {
			got = append(got, strings.Split(strings.TrimSpace(string(b)), "\n")...)
		}
	}
	if b, err := os.ReadFile(path); err == nil {
		got = append(got, strings.Split(strings.TrimSpace(string(b)), "\n")...)
	}
	if len(got) != 7 {
		t.Fatalf("want 7 records total, got %d: %v", len(got), got)
	}
	for i, l := range got {
		// 每条记录的 exit_code 即写入序号（0-6），位置即顺序
		if !strings.Contains(l, `"exit_code":`+strconv.Itoa(i)) {
			t.Errorf("line %d want exit_code=%d, got: %s", i, i, l)
		}
	}
}

// keep=0：不保留历史，轮转直接删当前文件（可配置的完全禁用历史模式）。
func TestAppendKeepZeroDeletes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	if err := Append(path, Record{Cmd: "a"}, 20, 0); err != nil {
		t.Fatal(err)
	}
	if err := Append(path, Record{Cmd: "b"}, 20, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Errorf("keep=0: .1 should not exist, err=%v", err)
	}
}

// maxSize=0：禁用轮转，文件持续追加、不产生历史份。
func TestAppendMaxSizeZeroDisablesRotate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	rec := Record{Action: "exec", Cmd: "ls"}
	for i := 0; i < 3; i++ {
		if err := Append(path, rec, 0, 5); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(data), "\n"); n != 3 {
		t.Errorf("current file want 3 lines, got %d: %s", n, data)
	}
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Errorf("maxSize=0: .1 should not exist, err=%v", err)
	}
}

// 已存在的过宽权限文件，追加后应收紧为 0600。
func TestAppendTightensExistingPerms(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Append(path, Record{Action: "exec", Cmd: "ls"}, 1024, 5); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("file perm = %o, want 600", fi.Mode().Perm())
	}
}
