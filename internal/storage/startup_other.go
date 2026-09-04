//go:build !windows

package storage

import "fmt"

func (s *Startup) Enabled() (bool, error) {
	return false, fmt.Errorf("storage: 当前平台不支持 Windows 自启动")
}

func (s *Startup) SetEnabled(bool) error {
	return fmt.Errorf("storage: 当前平台不支持 Windows 自启动")
}
