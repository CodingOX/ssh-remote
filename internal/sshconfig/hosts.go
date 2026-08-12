// Package sshconfig 从 OpenSSH 配置中列出 Host 别名（Spec §4.1）。
package sshconfig

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// DefaultConfigPath 返回用户默认 ssh config 路径。
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh", "config")
}

// ListHosts 解析主 config 中的 Host 行；排除含 * 或 ? 的模式。
// S2 最低要求：主文件 Host 行；Include 递归为 should（此处做一层 Include 尽力解析）。
func ListHosts(configPath string) ([]string, error) {
	if configPath == "" {
		configPath = DefaultConfigPath()
	}
	seen := map[string]struct{}{}
	var out []string
	if err := collectHosts(configPath, seen, &out, 0); err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	return out, nil
}

func collectHosts(path string, seen map[string]struct{}, out *[]string, depth int) error {
	if depth > 5 {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// 去掉行内注释
		if i := strings.Index(line, "#"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.ToLower(fields[0])
		switch key {
		case "host":
			for _, h := range fields[1:] {
				if strings.ContainsAny(h, "*?") {
					continue
				}
				if _, ok := seen[h]; ok {
					continue
				}
				seen[h] = struct{}{}
				*out = append(*out, h)
			}
		case "include":
			// 尽力展开 Include 路径（相对主文件目录）
			for _, pat := range fields[1:] {
				pat = expandTilde(pat)
				if !filepath.IsAbs(pat) {
					pat = filepath.Join(filepath.Dir(path), pat)
				}
				matches, err := filepath.Glob(pat)
				if err != nil {
					continue
				}
				for _, m := range matches {
					_ = collectHosts(m, seen, out, depth+1)
				}
			}
		}
	}
	return sc.Err()
}

func expandTilde(p string) string {
	if !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, p[2:])
}
