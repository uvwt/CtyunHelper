//go:build windows

package winui

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/uvwt/CtyunHelper/internal/buildinfo"
)

const (
	aboutOpenRepository = 2601
	aboutClose          = 2602
)

var (
	aboutClassOnce sync.Once
	aboutClassErr  error
	aboutDialogMu  sync.Mutex
	aboutDialog    *aboutDialogState
	shellExecuteW  = shell32.NewProc("ShellExecuteW")
)

type aboutDialogState struct {
	hwnd uintptr
}

func openAboutWindow(owner uintptr) {
	aboutDialogMu.Lock()
	if aboutDialog != nil && aboutDialog.hwnd != 0 {
		hwnd := aboutDialog.hwnd
		aboutDialogMu.Unlock()
		showWindow.Call(hwnd, swShow)
		setForegroundWindow.Call(hwnd)
		return
	}
	aboutDialogMu.Unlock()

	if err := ensureAboutWindowClass(); err != nil {
		showMessage(owner, "关于", err.Error(), mbIconError)
		return
	}

	instance, _, _ := getModuleHandleW.Call(0)
	hwnd, _, callErr := createWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(utf16Ptr("CtyunHelperAboutWindow"))),
		uintptr(unsafe.Pointer(utf16Ptr("关于 CtyunHelper"))),
		wsOverlappedWindow,
		cwUseDefault, cwUseDefault, 640, 320,
		owner, 0, instance, 0,
	)
	if hwnd == 0 {
		showMessage(owner, "关于", fmt.Sprintf("创建关于窗口失败: %v", callErr), mbIconError)
		return
	}

	state := &aboutDialogState{hwnd: hwnd}
	aboutDialogMu.Lock()
	aboutDialog = state
	aboutDialogMu.Unlock()

	icon := appIconLarge
	if icon == 0 {
		icon, _, _ = loadIconW.Call(0, idiApplication)
	}
	createAppIconControl(hwnd, instance, icon, 34, 34, 56, 56)
	createLabel(hwnd, instance, buildinfo.AppName, 112, 34, 450, 28)
	createLabel(hwnd, instance, buildinfo.DisplayName, 112, 66, 450, 26)

	createLabel(hwnd, instance, "版本："+buildinfo.Version, 34, 112, 520, 26)
	createLabel(hwnd, instance, "作者："+buildinfo.Author, 34, 146, 520, 26)
	createLabel(hwnd, instance, "GitHub："+buildinfo.RepositoryURL, 34, 180, 560, 26)

	createControl(
		"BUTTON", "打开 GitHub", wsChild|wsVisible|wsTabStop|bsPushButton,
		146, 230, 150, 38, hwnd, aboutOpenRepository, instance,
	)
	createControl(
		"BUTTON", "关闭", wsChild|wsVisible|wsTabStop|bsPushButton,
		330, 230, 130, 38, hwnd, aboutClose, instance,
	)

	showWindow.Call(hwnd, swShow)
	updateWindow.Call(hwnd)
}

func aboutWindowProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	aboutDialogMu.Lock()
	state := aboutDialog
	aboutDialogMu.Unlock()
	if state == nil || state.hwnd != hwnd {
		result, _, _ := defWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
		return result
	}

	switch message {
	case wmClose:
		destroyWindow.Call(hwnd)
		return 0
	case wmCommand:
		switch uint16(wParam & 0xffff) {
		case aboutOpenRepository:
			if err := openAboutRepository(hwnd); err != nil {
				showMessage(hwnd, "打开 GitHub", err.Error(), mbIconError)
			}
		case aboutClose:
			destroyWindow.Call(hwnd)
		}
		return 0
	case wmDestroy:
		aboutDialogMu.Lock()
		if aboutDialog == state {
			aboutDialog = nil
		}
		aboutDialogMu.Unlock()
		return 0
	}
	result, _, _ := defWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return result
}

func openAboutRepository(owner uintptr) error {
	operation := utf16Ptr("open")
	target := utf16Ptr(buildinfo.RepositoryURL)
	result, _, _ := shellExecuteW.Call(
		owner,
		uintptr(unsafe.Pointer(operation)),
		uintptr(unsafe.Pointer(target)),
		0, 0, swShow,
	)
	if result <= 32 {
		return fmt.Errorf("无法打开默认浏览器（ShellExecute=%d）", result)
	}
	return nil
}

func ensureAboutWindowClass() error {
	aboutClassOnce.Do(func() {
		aboutClassErr = registerDialogWindowClass("CtyunHelperAboutWindow", aboutWindowProc)
	})
	return aboutClassErr
}
