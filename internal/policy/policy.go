// Package policy 实现命令黑名单、写路径白名单与限额（Spec §8）。
package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Policy 生效中的策略快照。
type Policy struct {
	CommandTimeoutMs int64    `toml:"command_timeout_ms"`
	MaxOutputBytes   int      `toml:"max_output_bytes"`
	MaxFileBytes     int64    `toml:"max_file_bytes"`
	MaxCommandChars  int      `toml:"max_command_chars"`
	ReadOnly         bool     `toml:"read_only"`
	CommandAllowlist []string `toml:"command_allowlist"`
	CommandDenylist  []string `toml:"command_denylist"`
	WriteAllowlist   []string `toml:"write_allowlist"`
	ReadDenylist     []string `toml:"read_denylist"`
	LocalDenylist    []string `toml:"local_denylist"`

	denyRes      []*regexp.Regexp // 编译缓存
	allowlistRes []*regexp.Regexp // command_allowlist 编译缓存
}

// Default 返回内置安全默认（无 policy 文件时可用）。
func Default() *Policy {
	p := &Policy{
		CommandTimeoutMs: 60_000,
		MaxOutputBytes:   1_048_576,
		MaxFileBytes:     5_242_880,
		MaxCommandChars:  defaultMaxCommandChars(),
		CommandDenylist:  defaultDenylist(),
		WriteAllowlist:   []string{"/tmp/", "~/agent-drop/"},
		ReadDenylist:     defaultReadDenylist(),
		LocalDenylist:    defaultLocalDenylist(),
	}
	_ = p.compile()
	return p
}

// Load 加载默认并与文件合并；文件不存在则纯默认。
// 未知键会导致 TOML 解码失败（Strict 元数据检查）。
func Load(path string) (*Policy, error) {
	p := Default()
	if path == "" {
		return p, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return p, nil
		}
		return nil, err
	}
	// 严格：拒绝未知键，避免拼写导致策略静默失效
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse policy: %w", err)
	}
	allowed := map[string]struct{}{
		"command_timeout_ms": {},
		"max_output_bytes":   {},
		"max_file_bytes":     {},
		"max_command_chars":  {},
		"read_only":          {},
		"command_allowlist":  {},
		"command_denylist":   {},
		"write_allowlist":    {},
		"read_denylist":      {},
		"local_denylist":     {},
	}
	for k := range raw {
		if _, ok := allowed[k]; !ok {
			return nil, fmt.Errorf("unknown policy key: %s", k)
		}
	}
	// 覆盖已知字段
	var file Policy
	if err := toml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse policy: %w", err)
	}
	if _, ok := raw["command_timeout_ms"]; ok {
		p.CommandTimeoutMs = file.CommandTimeoutMs
	}
	if _, ok := raw["max_output_bytes"]; ok {
		p.MaxOutputBytes = file.MaxOutputBytes
	}
	if _, ok := raw["max_file_bytes"]; ok {
		p.MaxFileBytes = file.MaxFileBytes
	}
	if _, ok := raw["max_command_chars"]; ok {
		p.MaxCommandChars = file.MaxCommandChars
	}
	if _, ok := raw["read_only"]; ok {
		p.ReadOnly = file.ReadOnly
	}
	if _, ok := raw["command_allowlist"]; ok {
		// 覆盖式：文件提供的列表整表替换默认（空表表示不启用白名单）
		p.CommandAllowlist = file.CommandAllowlist
	}
	if _, ok := raw["command_denylist"]; ok {
		// 文件提供的列表与默认合并：文件替换整表（覆盖式）
		p.CommandDenylist = file.CommandDenylist
	}
	if _, ok := raw["write_allowlist"]; ok {
		p.WriteAllowlist = file.WriteAllowlist
	}
	if _, ok := raw["read_denylist"]; ok {
		p.ReadDenylist = file.ReadDenylist
	}
	if _, ok := raw["local_denylist"]; ok {
		p.LocalDenylist = file.LocalDenylist
	}
	if err := p.compile(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Policy) compile() error {
	p.denyRes = p.denyRes[:0]
	for _, pat := range p.CommandDenylist {
		re, err := regexp.Compile(pat)
		if err != nil {
			return fmt.Errorf("bad denylist pattern %q: %w", pat, err)
		}
		p.denyRes = append(p.denyRes, re)
	}
	p.allowlistRes = p.allowlistRes[:0]
	for _, pat := range p.CommandAllowlist {
		re, err := regexp.Compile(pat)
		if err != nil {
			return fmt.Errorf("bad allowlist pattern %q: %w", pat, err)
		}
		p.allowlistRes = append(p.allowlistRes, re)
	}
	return nil
}

// CheckCommandLength 校验命令串长度是否超过 max_command_chars。
func (p *Policy) CheckCommandLength(cmds ...string) error {
	limit := p.MaxCommandChars
	if limit <= 0 {
		limit = defaultMaxCommandChars()
	}
	for _, c := range cmds {
		if c == "" {
			continue
		}
		if len(c) > limit {
			return fmt.Errorf("command exceeds max length %d", limit)
		}
	}
	return nil
}

// CheckAllowlist 当 command_allowlist 非空时，命令须匹配至少一条白名单正则。
func (p *Policy) CheckAllowlist(cmds ...string) error {
	if len(p.allowlistRes) == 0 {
		return nil
	}
	for _, c := range cmds {
		if c == "" {
			continue
		}
		allowed := false
		for _, re := range p.allowlistRes {
			if re.MatchString(c) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("command not in allowlist")
		}
	}
	return nil
}

// CheckReadOnly 当 read_only 为 true 时拒绝写操作（Put 等）；Exec 请用 CheckReadOnlyCommand。
func (p *Policy) CheckReadOnly() error {
	if p.ReadOnly {
		return fmt.Errorf("policy is read-only")
	}
	return nil
}

// CheckReadOnlyCommand 在 read_only 模式下仅允许冻结的只读命令集合；ReadOnly=false 时不限制。
func (p *Policy) CheckReadOnlyCommand(cmd string) error {
	if !p.ReadOnly {
		return nil
	}
	return checkReadOnlyCommand(cmd)
}

// checkReadOnlyCommand 校验单条命令是否属于只读冻结集合；含 shell 控制符一律拒绝。
func checkReadOnlyCommand(cmd string) error {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return fmt.Errorf("read-only mode: empty command")
	}
	// 只读面禁止管道、替换、链式执行等 shell 控制结构
	for _, ch := range cmd {
		switch ch {
		case ';', '&', '|', '<', '>', '`', '$', '(', ')', '{', '}':
			return fmt.Errorf("read-only mode: shell control character %q not allowed", string(ch))
		}
	}
	words := strings.Fields(cmd)
	for len(words) > 0 {
		w := strings.ToLower(words[0])
		if _, ok := prefixWrappers[w]; ok {
			words = words[1:]
			continue
		}
		break
	}
	if len(words) == 0 {
		return fmt.Errorf("read-only mode: no command word")
	}
	if _, ok := readOnlySingleWords[strings.ToLower(words[0])]; ok {
		return nil
	}
	if len(words) >= 2 {
		two := strings.ToLower(words[0] + " " + words[1])
		if _, ok := readOnlyTwoWordPrefixes[two]; ok {
			return nil
		}
	}
	return fmt.Errorf("read-only mode: command %q not allowed", words[0])
}

// CheckCommand 对用户原始命令与最终发送串都匹配，任一命中即拒绝。
// 顺序：长度 → 白名单 → 黑名单（denylist + invoked-word）。
func (p *Policy) CheckCommand(cmds ...string) error {
	if err := p.CheckCommandLength(cmds...); err != nil {
		return err
	}
	if err := p.CheckAllowlist(cmds...); err != nil {
		return err
	}
	for _, c := range cmds {
		if c == "" {
			continue
		}
		for i, re := range p.denyRes {
			if re.MatchString(c) {
				return fmt.Errorf("command denied by denylist pattern %q", p.CommandDenylist[i])
			}
		}
		if err := checkInvokedDeny(c); err != nil {
			return err
		}
	}
	return nil
}

// invokedDenyWords 必须以 shell 命令位置出现才拒绝（避免 last reboot 等误杀）。
var invokedDenyWords = []string{"reboot", "shutdown", "halt", "poweroff", "eval"}

func checkInvokedDeny(cmd string) error {
	for _, word := range invokedDenyWords {
		if isInvokedWord(cmd, word) {
			return fmt.Errorf("command denied: invoked %q", word)
		}
	}
	return nil
}

// prefixWrappers 出现在段首时可跳过，继续找真正命令词。
var prefixWrappers = map[string]struct{}{
	"sudo": {}, "nohup": {}, "command": {}, "env": {}, "time": {}, "stdbuf": {},
}

func isInvokedWord(cmd, word string) bool {
	for _, seg := range splitCommandSegments(cmd) {
		if w := firstCommandWord(seg); strings.EqualFold(w, word) {
			return true
		}
	}
	return false
}

func splitCommandSegments(cmd string) []string {
	var segments []string
	var current strings.Builder
	for i := 0; i < len(cmd); i++ {
		if i+1 < len(cmd) && cmd[i:i+2] == "&&" {
			segments = append(segments, current.String())
			current.Reset()
			i++
			continue
		}
		if i+1 < len(cmd) && cmd[i:i+2] == "||" {
			segments = append(segments, current.String())
			current.Reset()
			i++
			continue
		}
		switch cmd[i] {
		case ';', '|', '&', '(', '{', '\n':
			segments = append(segments, current.String())
			current.Reset()
		default:
			current.WriteByte(cmd[i])
		}
	}
	segments = append(segments, current.String())
	return segments
}

func firstCommandWord(seg string) string {
	words := strings.Fields(strings.TrimSpace(seg))
	for len(words) > 0 {
		w := strings.ToLower(words[0])
		if _, ok := prefixWrappers[w]; ok {
			words = words[1:]
			continue
		}
		return words[0]
	}
	return ""
}

// CheckWritePath 校验 put 远端路径是否在白名单内。
// remoteHome 为远端 $HOME（可为空；若白名单含 ~/ 且 home 为空则无法匹配 ~ 项）。
func (p *Policy) CheckWritePath(remotePath, remoteHome string) error {
	if remotePath == "" {
		return fmt.Errorf("remote path is empty")
	}
	// 规范化：清理 . 与 ..；若仍含 .. 则拒绝
	clean := filepath.Clean(remotePath)
	// 远端路径按 POSIX 处理
	clean = strings.ReplaceAll(clean, "\\", "/")
	if clean == "." || clean == "" {
		return fmt.Errorf("remote path is empty")
	}
	if strings.Contains(clean, "..") {
		return fmt.Errorf("remote path must not contain ..")
	}
	// 相对路径（非 ~/ 开头且非绝对）拒绝，避免歧义
	if !strings.HasPrefix(clean, "/") && !strings.HasPrefix(remotePath, "~/") && remotePath != "~" {
		// 允许用户传入 ~/foo（用原始 remotePath 判断）
		if !strings.HasPrefix(remotePath, "~/") {
			return fmt.Errorf("remote path must be absolute or start with ~/")
		}
	}

	candidates := []string{clean}
	// 若用户写了 ~/x，展开后再匹配绝对前缀白名单
	if strings.HasPrefix(remotePath, "~/") || remotePath == "~" {
		if remoteHome == "" {
			return fmt.Errorf("cannot expand ~ without remote home")
		}
		rest := strings.TrimPrefix(remotePath, "~")
		rest = strings.TrimPrefix(rest, "/")
		expanded := filepath.Clean(remoteHome + "/" + rest)
		expanded = strings.ReplaceAll(expanded, "\\", "/")
		candidates = append(candidates, expanded)
		// 也保留 ~/ 形式用于匹配白名单中的 ~/ 条目
		candidates = append(candidates, remotePath)
	}

	for _, entry := range p.WriteAllowlist {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		// 展开白名单中的 ~/
		expandedEntry := entry
		if strings.HasPrefix(entry, "~/") {
			if remoteHome == "" {
				continue
			}
			expandedEntry = strings.ReplaceAll(
				filepath.Clean(remoteHome+"/"+strings.TrimPrefix(entry, "~/")),
				"\\", "/",
			)
			if strings.HasSuffix(entry, "/") && !strings.HasSuffix(expandedEntry, "/") {
				expandedEntry += "/"
			}
		}
		for _, cand := range candidates {
			if matchAllow(expandedEntry, cand) || matchAllow(entry, cand) {
				return nil
			}
		}
	}
	return fmt.Errorf("remote path not in write allowlist: %s", remotePath)
}

// matchAllow：条目以 / 结尾则前缀匹配，否则精确匹配（Spec：明确文件全路径或目录前缀）。
func matchAllow(entry, path string) bool {
	if entry == "" {
		return false
	}
	// ~/ 形式：用前缀语义
	if strings.HasPrefix(entry, "~/") {
		if strings.HasPrefix(path, "~/") || path == entry {
			if strings.HasSuffix(entry, "/") {
				return strings.HasPrefix(path, entry) || path+"/" == entry
			}
			return path == entry || strings.HasPrefix(path, entry+"/")
		}
	}
	if strings.HasSuffix(entry, "/") {
		return strings.HasPrefix(path, entry) || path+"/" == entry || path == strings.TrimSuffix(entry, "/")
	}
	return path == entry
}

// CheckReadPath 校验 get 远端路径是否命中 read_denylist。
// remoteHome 为远端 $HOME，用于展开 ~/ 条目。
func (p *Policy) CheckReadPath(remotePath, remoteHome string) error {
	if remotePath == "" {
		return fmt.Errorf("remote path is empty")
	}
	candidates, err := remotePathCandidates(remotePath, remoteHome)
	if err != nil {
		return err
	}
	for _, entry := range p.ReadDenylist {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		expandedEntry := expandHomeEntry(entry, remoteHome)
		for _, cand := range candidates {
			if matchDeny(entry, expandedEntry, cand) {
				return fmt.Errorf("remote read path denied: %s", remotePath)
			}
		}
	}
	// 硬编码安全网：任意路径含 /.ssh/ 段一律拒读（与 CheckLocalDest 对 .git 段同思路），
	// 不依赖 read_denylist 或 remoteHome 展开。
	for _, cand := range candidates {
		if containsSSHSegment(cand) {
			return fmt.Errorf("remote read path denied: %s", remotePath)
		}
	}
	return nil
}

func remotePathCandidates(remotePath, remoteHome string) ([]string, error) {
	clean := strings.ReplaceAll(filepath.Clean(remotePath), "\\", "/")
	if clean == "." || clean == "" {
		return nil, fmt.Errorf("remote path is empty")
	}
	if strings.Contains(clean, "..") {
		return nil, fmt.Errorf("remote path must not contain ..")
	}
	if !strings.HasPrefix(clean, "/") && !strings.HasPrefix(remotePath, "~/") && remotePath != "~" {
		if !strings.HasPrefix(remotePath, "~/") {
			return nil, fmt.Errorf("remote path must be absolute or start with ~/")
		}
	}
	candidates := []string{clean}
	if strings.HasPrefix(remotePath, "~/") || remotePath == "~" {
		if remoteHome == "" {
			return nil, fmt.Errorf("cannot expand ~ without remote home")
		}
		rest := strings.TrimPrefix(strings.TrimPrefix(remotePath, "~"), "/")
		expanded := strings.ReplaceAll(filepath.Clean(remoteHome+"/"+rest), "\\", "/")
		candidates = append(candidates, expanded, remotePath)
	}
	return candidates, nil
}

func expandHomeEntry(entry, home string) string {
	if !strings.HasPrefix(entry, "~/") || home == "" {
		return entry
	}
	expanded := strings.ReplaceAll(
		filepath.Clean(home+"/"+strings.TrimPrefix(entry, "~/")),
		"\\", "/",
	)
	if strings.HasSuffix(entry, "/") && !strings.HasSuffix(expanded, "/") {
		expanded += "/"
	}
	return expanded
}

// matchDeny：authorized_keys 为后缀匹配；条目以 / 结尾为前缀；否则精确。
func matchDeny(entry, expandedEntry, path string) bool {
	if entry == "authorized_keys" {
		return strings.HasSuffix(path, "authorized_keys") || strings.HasSuffix(path, "/authorized_keys")
	}
	for _, e := range []string{expandedEntry, entry} {
		if e == "" {
			continue
		}
		if strings.HasPrefix(e, "~/") {
			if strings.HasSuffix(e, "/") {
				if strings.HasPrefix(path, e) {
					return true
				}
			} else if path == e || strings.HasPrefix(path, e+"/") {
				return true
			}
			continue
		}
		if strings.HasSuffix(e, "/") {
			if strings.HasPrefix(path, e) || path+"/" == e || path == strings.TrimSuffix(e, "/") {
				return true
			}
			continue
		}
		if path == e {
			return true
		}
	}
	return false
}

// CheckLocalSource 校验 put 本机源路径是否命中 local_denylist。
func (p *Policy) CheckLocalSource(localPath, homeDir string) error {
	if localPath == "" {
		return fmt.Errorf("local path is empty")
	}
	clean := filepath.Clean(localPath)
	if err := p.checkLocalDenylist(clean, homeDir); err != nil {
		return err
	}
	return nil
}

// CheckLocalDest 校验 get 本机目标路径：local_denylist 与仓库 .git/ 均拒绝。
func (p *Policy) CheckLocalDest(localPath, homeDir, repoRoot string) error {
	if localPath == "" {
		return fmt.Errorf("local path is empty")
	}
	clean := filepath.Clean(localPath)
	if err := p.checkLocalDenylist(clean, homeDir); err != nil {
		return err
	}
	if isRepoGitPath(clean, repoRoot) || containsGitSegment(clean) {
		return fmt.Errorf("local dest path denied: %s", localPath)
	}
	return nil
}

func (p *Policy) checkLocalDenylist(localPath, homeDir string) error {
	path := filepath.ToSlash(localPath)
	for _, entry := range p.LocalDenylist {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		expandedEntry := expandHomeEntry(entry, homeDir)
		if matchDeny(entry, expandedEntry, path) {
			return fmt.Errorf("local path denied: %s", localPath)
		}
	}
	return nil
}

// containsGitSegment 拒绝任意路径中的 /.git/ 段（含末尾 /.git）。
func containsGitSegment(path string) bool {
	norm := filepath.ToSlash(filepath.Clean(path))
	if strings.Contains(norm, "/.git/") {
		return true
	}
	return strings.HasSuffix(norm, "/.git")
}

// containsSSHSegment 拒绝任意路径中的 /.ssh/ 段（含末尾 /.ssh）。
func containsSSHSegment(path string) bool {
	norm := filepath.ToSlash(filepath.Clean(path))
	if strings.Contains(norm, "/.ssh/") {
		return true
	}
	return strings.HasSuffix(norm, "/.ssh")
}

// isRepoGitPath 拒绝写入仓库根下的 .git 目录。
func isRepoGitPath(path, repoRoot string) bool {
	if repoRoot == "" {
		return false
	}
	repoRoot = filepath.Clean(repoRoot)
	clean := filepath.Clean(path)
	rel, err := filepath.Rel(repoRoot, clean)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	return rel == ".git" || strings.HasPrefix(rel, ".git/")
}

// DefaultPolicyPath 返回用户级默认 policy 路径。
func DefaultPolicyPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "ssh-remote", "policy.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "ssh-remote", "policy.toml")
}
