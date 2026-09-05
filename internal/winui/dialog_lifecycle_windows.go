//go:build windows

package winui

// closeAuxiliaryWindows 在主窗口退出前显式销毁所有辅助顶层窗口。各窗口的
// WM_DESTROY 会负责清理自身状态和事件订阅；这里取句柄时不持锁调用
// DestroyWindow，避免窗口过程再次取得同一把 mutex 造成死锁。
func closeAuxiliaryWindows() {
	handles := make([]uintptr, 0, 6)

	dialogMu.Lock()
	if loginDialog != nil && loginDialog.hwnd != 0 {
		handles = append(handles, loginDialog.hwnd)
	}
	if bindingDialog != nil && bindingDialog.hwnd != 0 {
		handles = append(handles, bindingDialog.hwnd)
	}
	dialogMu.Unlock()

	redeemDialogMu.Lock()
	if redeemDialog != nil && redeemDialog.hwnd != 0 {
		handles = append(handles, redeemDialog.hwnd)
	}
	redeemDialogMu.Unlock()

	settingsDialogMu.Lock()
	if settingsDialog != nil && settingsDialog.hwnd != 0 {
		handles = append(handles, settingsDialog.hwnd)
	}
	settingsDialogMu.Unlock()

	logsDialogMu.Lock()
	if logsDialog != nil && logsDialog.hwnd != 0 {
		handles = append(handles, logsDialog.hwnd)
	}
	logsDialogMu.Unlock()

	aboutDialogMu.Lock()
	if aboutDialog != nil && aboutDialog.hwnd != 0 {
		handles = append(handles, aboutDialog.hwnd)
	}
	aboutDialogMu.Unlock()

	for _, hwnd := range handles {
		destroyWindow.Call(hwnd)
	}
}
