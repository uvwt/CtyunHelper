//go:build !windows

package winui

import "fmt"

func Run() error {
	return fmt.Errorf("Windows UI 仅支持 Windows")
}
