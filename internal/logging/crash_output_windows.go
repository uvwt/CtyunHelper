//go:build windows

package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

// RedirectCrashOutput 把 Windows GUI 进程的 stderr 指向持久文件。
// Go runtime 的未捕获 panic 也会读取 STD_ERROR_HANDLE，因此漏网 panic 仍能留下完整堆栈。
func RedirectCrashOutput(path string) (*os.File, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("logging: crash 日志路径不能为空")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("logging: 创建 crash 日志目录: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("logging: 打开 crash 日志: %w", err)
	}
	if err := windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(file.Fd())); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("logging: 重定向 stderr: %w", err)
	}
	os.Stderr = file
	_, _ = fmt.Fprintf(file, "\n=== process start %s pid=%d ===\n", time.Now().Format(time.RFC3339), os.Getpid())
	return file, nil
}
