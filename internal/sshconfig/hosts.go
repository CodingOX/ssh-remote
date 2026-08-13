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

// HostEntry 表示一个非通配 Host 别名及其寻址字段（不探测网络）。
type HostEntry struct {
	Host      string
	User      string
	HostName  string
	Port      string
	ProxyJump string
}

// ListHostEntries 解析 config，返回每个非通配 Host 别名的寻址字段。
func ListHostEntries(configPath string) ([]HostEntry, error) {
	if configPath == "" {
		configPath = DefaultConfigPath()
	}
	seen := map[string]struct{}{}
	var out []HostEntry
	if err := collectHostEntries(configPath, seen, &out, 0); err != nil {
		if os.IsNotExist(err) {
			return []HostEntry{}, nil
		}
		return nil, err
	}
	return out, nil
}

// hostBlock 表示一个 Host 块内的别名与寻址关键字。
type hostBlock struct {
	aliases   []string
	user      string
	hostName  string
	port      string
	proxyJump string
}

func collectHostEntries(path string, seen map[string]struct{}, out *[]HostEntry, depth int) error {
	if depth > 5 {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	var block *hostBlock
	flushBlock := func() {
		if block == nil {
			return
		}
		for _, alias := range block.aliases {
			if strings.ContainsAny(alias, "*?") {
				continue
			}
			if _, ok := seen[alias]; ok {
				continue
			}
			seen[alias] = struct{}{}
			*out = append(*out, HostEntry{
				Host:      alias,
				User:      block.user,
				HostName:  block.hostName,
				Port:      block.port,
				ProxyJump: block.proxyJump,
			})
		}
		block = nil
	}

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.Index(line, "#"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.ToLower(fields[0])
		val := strings.Join(fields[1:], " ")
		switch key {
		case "host":
			flushBlock()
			block = &hostBlock{aliases: fields[1:]}
		case "include":
			flushBlock()
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
					_ = collectHostEntries(m, seen, out, depth+1)
				}
			}
		default:
			if block == nil {
				continue
			}
			switch key {
			case "user":
				block.user = val
			case "hostname":
				block.hostName = val
			case "port":
				block.port = val
			case "proxyjump":
				block.proxyJump = val
			}
		}
	}
	flushBlock()
	return sc.Err()
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
