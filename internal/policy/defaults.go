package policy

// defaultMaxCommandChars 单条命令最大字符数（Spec §8）。
func defaultMaxCommandChars() int {
	return 5000
}

// defaultReadDenylist 远端 get 默认拒绝路径（前缀、精确或后缀，见 matchDeny）。
func defaultReadDenylist() []string {
	return []string{
		"~/.ssh/",
		"/etc/shadow",
		"authorized_keys",
	}
}

// defaultLocalDenylist 本机 put 源与 get 目标默认拒绝路径。
func defaultLocalDenylist() []string {
	return []string{
		"~/.ssh/",
	}
}

func defaultDenylist() []string {
	return []string{
		// 远程管道拉取并执行
		`(?i)curl\b.*\|\s*sh\b`,
		`(?i)wget\b.*\|\s*sh\b`,
		// 篡改 SSH 授权密钥
		`(?i)(>>?)\s*.*\.ssh/authorized_keys\b`,
		// 持久化后门：cron / systemd
		`(?i)(>>?)\s*/etc/cron`,
		`(?i)(>>?)\s*/etc/systemd`,
		// 清空防火墙规则
		`(?i)\biptables\b[^\n|;&]*\s-F\b`,
		// 根目录毁灭性删除
		`(?i)rm\s+(-[a-zA-Z]*r[a-zA-Z]*f|-[a-zA-Z]*f[a-zA-Z]*r)\s+(/|/\*|~|/home\s*$|/home/)`,
		`(?i)rm\s+(-[a-zA-Z]*r[a-zA-Z]*f|-[a-zA-Z]*f[a-zA-Z]*r)\s+--\s*/`,
		// 磁盘设备
		`(?i)mkfs(\.|` + `\s|$)`,
		`(?i)dd\s+.*\bof\s*=\s*/dev/`,
		`(?i)wipefs\b`,
		// fork 炸弹
		`:\(\)\s*\{\s*:\|:&\s*\}\s*;\s*:`,
		// 重定向写块设备
		`(?i)>\s*/dev/sd[a-z]`,
		`(?i)>\s*/dev/nvme`,
		// 危险权限
		`(?i)chmod\s+(-R\s+)?777\s+/`,
		// 全库/全盘类
		`(?i)\bmkfs\.[a-z0-9]+\b`,
	}
}

// readOnlySingleWords 只读模式下允许的单命令词（去掉 sudo 等前缀后）。
var readOnlySingleWords = map[string]struct{}{
	"ls": {}, "cat": {}, "grep": {}, "find": {}, "stat": {}, "df": {}, "du": {},
	"head": {}, "tail": {}, "wc": {}, "ps": {}, "uname": {}, "uptime": {},
	"hostname": {}, "id": {}, "who": {}, "whoami": {}, "date": {}, "env": {},
	"pwd": {}, "echo": {}, "printf": {}, "which": {}, "file": {}, "free": {},
	"ss": {}, "journalctl": {},
}

// readOnlyTwoWordPrefixes 只读模式下允许的两词前缀（cmd + subcmd）。
var readOnlyTwoWordPrefixes = map[string]struct{}{
	"systemctl status": {},
	"git status":       {},
	"git log":          {},
	"git diff":         {},
	"git show":         {},
	"git branch":       {},
}
