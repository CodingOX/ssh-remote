// ssh-remote：Skill 友好的 OpenSSH 薄封装 CLI（Spec S2）。
package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/CodingOX/ssh-remote/internal/app"
	"github.com/CodingOX/ssh-remote/internal/response"
	"github.com/CodingOX/ssh-remote/internal/version"
)

func main() {
	cfg, args, err := parseGlobal(os.Args[1:])
	if err != nil {
		writeUsage(err.Error())
		return
	}
	if len(args) == 0 {
		writeUsage("subcommand required: hosts | exec | get | put")
		return
	}

	if err := cfg.LoadPolicy(); err != nil {
		response.WriteAndExit(response.Envelope{
			OK:     false,
			Action: args[0],
			Error:  &response.ErrorBody{Code: response.CodeUsage, Message: "policy: " + err.Error()},
			Meta: response.Meta{
				Version:   version.Version,
				Host:      nil,
				TimeoutMs: 0,
			},
			Result: nil,
		})
		return
	}

	sub := args[0]
	rest := args[1:]

	var env response.Envelope
	switch sub {
	case "hosts":
		env = app.Hosts(cfg)
	case "exec":
		host, cmd, err := parseExec(rest)
		if err != nil {
			env = usageEnv("exec", err.Error())
		} else {
			env = app.Exec(cfg, host, cmd)
		}
	case "get":
		host, remote, local, err := parseGet(rest)
		if err != nil {
			env = usageEnv("get", err.Error())
		} else {
			env = app.Get(cfg, host, remote, local)
		}
	case "put":
		host, local, remote, err := parsePut(rest)
		if err != nil {
			env = usageEnv("put", err.Error())
		} else {
			env = app.Put(cfg, host, local, remote)
		}
	case "version", "--version", "-V":
		fmt.Println(version.Version)
		os.Exit(0)
	case "help", "-h", "--help":
		fmt.Fprint(os.Stderr, helpText())
		os.Exit(0)
	default:
		env = usageEnv(sub, "unknown subcommand: "+sub)
	}

	response.WriteAndExit(env)
}

func usageEnv(action, msg string) response.Envelope {
	return response.Envelope{
		OK:     false,
		Action: action,
		Error:  &response.ErrorBody{Code: response.CodeUsage, Message: msg},
		Meta: response.Meta{
			Version:   version.Version,
			Host:      nil,
			TimeoutMs: 0,
		},
		Result: nil,
	}
}

func writeUsage(msg string) {
	response.WriteAndExit(usageEnv("usage", msg))
}

func helpText() string {
	return `ssh-remote — thin OpenSSH wrapper for agent skills (S2)

Usage:
  ssh-remote [global flags] hosts
  ssh-remote [global flags] exec <host> -- <command...>
  ssh-remote [global flags] get  <host> <remote-path> [local-path]
  ssh-remote [global flags] put  <host> <local-path> <remote-path>

Global flags:
  --config <path>    ssh config file (-F)
  --policy <path>    policy.toml path
  --timeout <dur>    e.g. 30s, 2m
  --workdir <path>   remote cd before exec
`
}

// parseGlobal 解析全局 flag，返回剩余 args。
func parseGlobal(argv []string) (*app.Config, []string, error) {
	cfg := &app.Config{}
	i := 0
	for i < len(argv) {
		a := argv[i]
		if a == "--" {
			i++
			break
		}
		if !strings.HasPrefix(a, "-") {
			break
		}
		switch {
		case a == "--config" && i+1 < len(argv):
			cfg.SSHConfig = argv[i+1]
			i += 2
		case strings.HasPrefix(a, "--config="):
			cfg.SSHConfig = strings.TrimPrefix(a, "--config=")
			i++
		case a == "--policy" && i+1 < len(argv):
			cfg.PolicyPath = argv[i+1]
			i += 2
		case strings.HasPrefix(a, "--policy="):
			cfg.PolicyPath = strings.TrimPrefix(a, "--policy=")
			i++
		case a == "--timeout" && i+1 < len(argv):
			d, err := time.ParseDuration(argv[i+1])
			if err != nil {
				return nil, nil, fmt.Errorf("invalid --timeout: %w", err)
			}
			cfg.Timeout = d
			i += 2
		case strings.HasPrefix(a, "--timeout="):
			d, err := time.ParseDuration(strings.TrimPrefix(a, "--timeout="))
			if err != nil {
				return nil, nil, fmt.Errorf("invalid --timeout: %w", err)
			}
			cfg.Timeout = d
			i++
		case a == "--workdir" && i+1 < len(argv):
			cfg.Workdir = argv[i+1]
			i += 2
		case strings.HasPrefix(a, "--workdir="):
			cfg.Workdir = strings.TrimPrefix(a, "--workdir=")
			i++
		case a == "-h", a == "--help":
			return cfg, []string{"help"}, nil
		default:
			return nil, nil, fmt.Errorf("unknown flag: %s", a)
		}
	}
	return cfg, argv[i:], nil
}

func parseExec(args []string) (host string, cmd []string, err error) {
	if len(args) < 1 {
		return "", nil, fmt.Errorf("usage: exec <host> -- <command...>")
	}
	host = args[0]
	rest := args[1:]
	// 跳过可选 --
	if len(rest) > 0 && rest[0] == "--" {
		rest = rest[1:]
	}
	if len(rest) == 0 {
		return host, nil, fmt.Errorf("command is required")
	}
	return host, rest, nil
}

func parseGet(args []string) (host, remote, local string, err error) {
	if len(args) < 2 {
		return "", "", "", fmt.Errorf("usage: get <host> <remote-path> [local-path]")
	}
	host, remote = args[0], args[1]
	if len(args) >= 3 {
		local = args[2]
	}
	return host, remote, local, nil
}

func parsePut(args []string) (host, local, remote string, err error) {
	if len(args) < 3 {
		return "", "", "", fmt.Errorf("usage: put <host> <local-path> <remote-path>")
	}
	return args[0], args[1], args[2], nil
}
