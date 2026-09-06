//go:build windows

package winui

import (
	"errors"
	"fmt"

	"github.com/tailscale/win"
	"github.com/uvwt/CtyunHelper/internal/buildinfo"
	"golang.org/x/sys/windows"
)

func acquireSingleInstance() (windows.Handle, bool, error) {
	name, err := windows.UTF16PtrFromString(`Local\CtyunHelper`)
	if err != nil {
		return 0, false, fmt.Errorf("创建单实例名称: %w", err)
	}
	handle, err := windows.CreateMutex(nil, false, name)
	if handle == 0 {
		return 0, false, fmt.Errorf("创建单实例 Mutex: %w", err)
	}
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return handle, true, nil
	}
	if err != nil {
		_ = windows.CloseHandle(handle)
		return 0, false, fmt.Errorf("创建单实例 Mutex: %w", err)
	}
	return handle, false, nil
}

func activateExistingWalkWindow() {
	title, err := windows.UTF16PtrFromString(walkWindowTitle)
	if err != nil {
		return
	}
	hwnd := win.FindWindow(nil, title)
	if hwnd == 0 {
		return
	}
	win.ShowWindow(hwnd, win.SW_SHOW)
	win.SetForegroundWindow(hwnd)
}

func openAboutRepository(owner uintptr) error {
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(buildinfo.RepositoryURL)
	if err != nil {
		return err
	}
	if !win.ShellExecute(win.HWND(owner), verb, target, nil, nil, win.SW_SHOW) {
		return fmt.Errorf("无法打开默认浏览器")
	}
	return nil
}

func ShowError(title, message string) {
	caption, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	text, err := windows.UTF16PtrFromString(message)
	if err != nil {
		return
	}
	win.MessageBox(0, text, caption, win.MB_OK|win.MB_ICONERROR|win.MB_SETFOREGROUND)
}
