package policy

// defaultDenylist 内置高危命令模式：防误操作，不宣称防专业绕过。
func defaultDenylist() []string {
	return []string{
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
