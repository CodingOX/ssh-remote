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
	CommandDenylist  []string `toml:"command_denylist"`
	WriteAllowlist   []string `toml:"write_allowlist"`

	denyRes []*regexp.Regexp // 编译缓存
}

// Default 返回内置安全默认（无 policy 文件时可用）。
func Default() *Policy {
	p := &Policy{
		CommandTimeoutMs: 60_000,
		MaxOutputBytes:   1_048_576,
		MaxFileBytes:     5_242_880,
		CommandDenylist:  defaultDenylist(),
		WriteAllowlist:   []string{"/tmp/", "~/agent-drop/"},
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
		"command_denylist":   {},
		"write_allowlist":    {},
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
	if _, ok := raw["command_denylist"]; ok {
		// 文件提供的列表与默认合并：文件替换整表（覆盖式）
		p.CommandDenylist = file.CommandDenylist
	}
	if _, ok := raw["write_allowlist"]; ok {
		p.WriteAllowlist = file.WriteAllowlist
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
	return nil
}

// CheckCommand 对用户原始命令与最终发送串都匹配，任一命中即拒绝。
func (p *Policy) CheckCommand(cmds ...string) error {
	for _, c := range cmds {
		if c == "" {
			continue
		}
		for i, re := range p.denyRes {
			if re.MatchString(c) {
				return fmt.Errorf("command denied by denylist pattern %q", p.CommandDenylist[i])
			}
		}
	}
	return nil
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
