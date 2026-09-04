//go:build windows

package winui

import (
	"fmt"
	"strings"
	"sync"
	"unsafe"

	"github.com/uvwt/CtyunHelper/internal/app"
)

const (
	settingsAutomation = 2401
	settingsStartup    = 2402
	settingsSave       = 2403

	wmSettingsSaved = wmApp + 30
	wmLogsRefresh   = wmApp + 31

	settingsCheckboxStyle = 0x00000003
	esMultiline           = 0x0004
	esAutoVScroll         = 0x0040
	esReadOnly            = 0x0800
	wsVScroll             = 0x00200000
)

var (
	settingsClassOnce sync.Once
	settingsClassErr  error
	settingsDialogMu  sync.Mutex
	settingsDialog    *settingsDialogState

	logsClassOnce sync.Once
	logsClassErr  error
	logsDialogMu  sync.Mutex
	logsDialog    *logsDialogState
)

type settingsDialogState struct {
	mu sync.Mutex

	hwnd            uintptr
	automationCheck uintptr
	startupCheck    uintptr
	saveButton      uintptr
	resultErr       error
	busy            bool
}

type logsDialogState struct {
	hwnd        uintptr
	logEdit     uintptr
	unsubscribe func()
}

func openSettingsWindow(owner uintptr) {
	if uiRuntime == nil {
		return
	}
	settingsDialogMu.Lock()
	if settingsDialog != nil && settingsDialog.hwnd != 0 {
		hwnd := settingsDialog.hwnd
		settingsDialogMu.Unlock()
		showWindow.Call(hwnd, swShow)
		setForegroundWindow.Call(hwnd)
		return
	}
	settingsDialogMu.Unlock()

	if err := ensureSettingsWindowClass(); err != nil {
		showMessage(owner, "设置", err.Error(), mbIconError)
		return
	}
	current, err := uiRuntime.CurrentSettings()
	if err != nil {
		showMessage(owner, "设置", err.Error(), mbIconError)
		return
	}
	instance, _, _ := getModuleHandleW.Call(0)
	hwnd, _, callErr := createWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(utf16Ptr("CtyunHelperSettingsWindow"))),
		uintptr(unsafe.Pointer(utf16Ptr("CtyunHelper 设置"))),
		wsOverlappedWindow,
		cwUseDefault, cwUseDefault, 560, 330,
		owner, 0, instance, 0,
	)
	if hwnd == 0 {
		showMessage(owner, "设置", fmt.Sprintf("创建设置窗口失败: %v", callErr), mbIconError)
		return
	}
	state := &settingsDialogState{hwnd: hwnd}
	state.automationCheck = createControl(
		"BUTTON", "启用 AI / 兑换自动任务", wsChild|wsVisible|wsTabStop|settingsCheckboxStyle,
		36, 48, 300, 30, hwnd, settingsAutomation, instance,
	)
	state.startupCheck = createControl(
		"BUTTON", "Windows 登录后自动启动 CtyunHelper", wsChild|wsVisible|wsTabStop|settingsCheckboxStyle,
		36, 96, 380, 30, hwnd, settingsStartup, instance,
	)
	createLabel(hwnd, instance, "自启动使用当前用户注册表，不需要管理员权限。", 36, 140, 450, 26)
	state.saveButton = createControl(
		"BUTTON", "保存", wsChild|wsVisible|wsTabStop|bsPushButton,
		190, 205, 140, 38, hwnd, settingsSave, instance,
	)
	if current.AutomationEnabled {
		sendMessageW.Call(state.automationCheck, bmSetCheck, bstChecked, 0)
	}
	if current.StartOnLogin {
		sendMessageW.Call(state.startupCheck, bmSetCheck, bstChecked, 0)
	}
	settingsDialogMu.Lock()
	settingsDialog = state
	settingsDialogMu.Unlock()
	showWindow.Call(hwnd, swShow)
	updateWindow.Call(hwnd)
}

func settingsWindowProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	settingsDialogMu.Lock()
	state := settingsDialog
	settingsDialogMu.Unlock()
	if state == nil || state.hwnd != hwnd {
		result, _, _ := defWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
		return result
	}

	switch message {
	case wmClose:
		destroyWindow.Call(hwnd)
		return 0
	case wmCommand:
		if uint16(wParam&0xffff) == settingsSave {
			startSettingsSave(state)
		}
		return 0
	case wmSettingsSaved:
		state.mu.Lock()
		err := state.resultErr
		state.resultErr = nil
		state.busy = false
		state.mu.Unlock()
		enableWindow.Call(state.saveButton, 1)
		if err != nil {
			showMessage(hwnd, "保存设置", err.Error(), mbIconError)
			return 0
		}
		showMessage(hwnd, "设置", "设置已保存并立即生效。", mbInformation)
		destroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		settingsDialogMu.Lock()
		if settingsDialog == state {
			settingsDialog = nil
		}
		settingsDialogMu.Unlock()
		return 0
	}
	result, _, _ := defWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return result
}

func startSettingsSave(state *settingsDialogState) {
	if !beginDialogWork(&state.mu, &state.busy) {
		return
	}
	automationChecked, _, _ := sendMessageW.Call(state.automationCheck, bmGetCheck, 0, 0)
	startupChecked, _, _ := sendMessageW.Call(state.startupCheck, bmGetCheck, 0, 0)
	settings := app.GeneralSettings{
		AutomationEnabled: automationChecked == bstChecked,
		StartOnLogin:      startupChecked == bstChecked,
	}
	enableWindow.Call(state.saveButton, 0)
	go func() {
		err := uiRuntime.SaveSettings(settings)
		state.mu.Lock()
		state.resultErr = err
		state.mu.Unlock()
		postMessageW.Call(state.hwnd, wmSettingsSaved, 0, 0)
	}()
}

func openLogsWindow(owner uintptr) {
	if uiRuntime == nil || uiModel == nil {
		return
	}
	logsDialogMu.Lock()
	if logsDialog != nil && logsDialog.hwnd != 0 {
		state := logsDialog
		logsDialogMu.Unlock()
		refreshLogsWindow(state)
		showWindow.Call(state.hwnd, swShow)
		setForegroundWindow.Call(state.hwnd)
		return
	}
	logsDialogMu.Unlock()

	if err := ensureLogsWindowClass(); err != nil {
		showMessage(owner, "日志", err.Error(), mbIconError)
		return
	}
	instance, _, _ := getModuleHandleW.Call(0)
	hwnd, _, callErr := createWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(utf16Ptr("CtyunHelperLogsWindow"))),
		uintptr(unsafe.Pointer(utf16Ptr("CtyunHelper 日志"))),
		wsOverlappedWindow,
		cwUseDefault, cwUseDefault, 920, 640,
		owner, 0, instance, 0,
	)
	if hwnd == 0 {
		showMessage(owner, "日志", fmt.Sprintf("创建日志窗口失败: %v", callErr), mbIconError)
		return
	}
	createLabel(hwnd, instance, "日志文件："+uiRuntime.LogPath(), 20, 20, 850, 26)
	state := &logsDialogState{hwnd: hwnd}
	state.logEdit = createControl(
		"EDIT", "", wsChild|wsVisible|wsBorder|wsVScroll|esMultiline|esAutoVScroll|esReadOnly,
		20, 54, 850, 520, hwnd, 0, instance,
	)
	events, unsubscribe := uiModel.Events().Subscribe(64)
	state.unsubscribe = unsubscribe
	logsDialogMu.Lock()
	logsDialog = state
	logsDialogMu.Unlock()
	refreshLogsWindow(state)
	go func(window uintptr, source <-chan app.Event) {
		for event := range source {
			if event.Type == app.EventLogAdded {
				postMessageW.Call(window, wmLogsRefresh, 0, 0)
			}
		}
	}(hwnd, events)
	showWindow.Call(hwnd, swShow)
	updateWindow.Call(hwnd)
}

func logsWindowProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	logsDialogMu.Lock()
	state := logsDialog
	logsDialogMu.Unlock()
	if state == nil || state.hwnd != hwnd {
		result, _, _ := defWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
		return result
	}

	switch message {
	case wmClose:
		destroyWindow.Call(hwnd)
		return 0
	case wmLogsRefresh:
		refreshLogsWindow(state)
		return 0
	case wmDestroy:
		if state.unsubscribe != nil {
			state.unsubscribe()
			state.unsubscribe = nil
		}
		logsDialogMu.Lock()
		if logsDialog == state {
			logsDialog = nil
		}
		logsDialogMu.Unlock()
		return 0
	}
	result, _, _ := defWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return result
}

func refreshLogsWindow(state *logsDialogState) {
	if state == nil || state.logEdit == 0 || uiRuntime == nil {
		return
	}
	entries := uiRuntime.LogSnapshot(300)
	if len(entries) == 0 {
		setControlText(state.logEdit, "暂无运行日志。")
		return
	}
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		lines = append(lines, entry.Line())
	}
	setControlText(state.logEdit, strings.Join(lines, "\r\n"))
}

func ensureSettingsWindowClass() error {
	settingsClassOnce.Do(func() {
		settingsClassErr = registerDialogWindowClass("CtyunHelperSettingsWindow", settingsWindowProc)
	})
	return settingsClassErr
}

func ensureLogsWindowClass() error {
	logsClassOnce.Do(func() {
		logsClassErr = registerDialogWindowClass("CtyunHelperLogsWindow", logsWindowProc)
	})
	return logsClassErr
}
