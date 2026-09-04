package storage

import (
	"fmt"
	"strings"
)

const startupValueName = "CtyunHelper"

type Startup struct {
	executable string
}

func NewStartup(executable string) (*Startup, error) {
	executable = strings.TrimSpace(executable)
	if executable == "" {
		return nil, fmt.Errorf("storage: 自启动程序路径不能为空")
	}
	if strings.ContainsRune(executable, '"') {
		return nil, fmt.Errorf("storage: 自启动程序路径包含非法引号")
	}
	return &Startup{executable: executable}, nil
}

func (s *Startup) Command() string {
	if s == nil {
		return ""
	}
	// Windows Run 值总是对 exe 路径加引号，路径含空格时不会被拆成错误命令；
	// 即使当前路径没有空格也保持同一规范形式，便于精确检测旧/错误启动项。
	return `"` + s.executable + `"`
}
