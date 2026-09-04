//go:build windows

package winui

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"github.com/uvwt/CtyunHelper/internal/app"
)

const (
	csHRedraw          = 0x0002
	csVRedraw          = 0x0001
	cwUseDefault       = ^uintptr(0x7fffffff)
	wmDestroy          = 0x0002
	wmClose            = 0x0010
	wmCommand          = 0x0111
	wmLButtonDblClk    = 0x0203
	wmRButtonUp        = 0x0205
	wmApp              = 0x8000
	wmTray             = wmApp + 1
	wmStateChanged     = wmApp + 2
	wsOverlappedWindow = 0x00CF0000
	wsVisible          = 0x10000000
	wsChild            = 0x40000000
	ssLeft             = 0x00000000
	bsPushButton       = 0x00000000
	swHide             = 0
	swShow             = 5
	colorWindow        = 5
	idiApplication     = 32512
	idcArrow           = 32512
	mfString           = 0x0000
	mfSeparator        = 0x0800
	tpmRightButton     = 0x0002
	tpmReturnCmd       = 0x0100
	nimAdd             = 0x00000000
	nimDelete          = 0x00000002
	nifMessage         = 0x00000001
	nifIcon            = 0x00000002
	nifTip             = 0x00000004
	menuOpen           = 1001
	menuRunAI          = 1002
	menuExit           = 1003
	buttonLogin        = 1101
	buttonBind         = 1102
	buttonAI           = 1103
	errorAlreadyExists = 183
)

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSmall  uintptr
}

type msg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	PtX     int32
	PtY     int32
}

type point struct {
	X int32
	Y int32
}

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

type notifyIconData struct {
	Size             uint32
	HWnd             uintptr
	ID               uint32
	Flags            uint32
	CallbackMessage  uint32
	Icon             uintptr
	Tip              [128]uint16
	State            uint32
	StateMask        uint32
	Info             [256]uint16
	TimeoutOrVersion uint32
	InfoTitle        [64]uint16
	InfoFlags        uint32
	GUID             guid
	BalloonIcon      uintptr
}

var (
	user32              = syscall.NewLazyDLL("user32.dll")
	kernel32UI          = syscall.NewLazyDLL("kernel32.dll")
	shell32             = syscall.NewLazyDLL("shell32.dll")
	registerClassExW    = user32.NewProc("RegisterClassExW")
	createWindowExW     = user32.NewProc("CreateWindowExW")
	defWindowProcW      = user32.NewProc("DefWindowProcW")
	showWindow          = user32.NewProc("ShowWindow")
	updateWindow        = user32.NewProc("UpdateWindow")
	setWindowTextW      = user32.NewProc("SetWindowTextW")
	enableWindow        = user32.NewProc("EnableWindow")
	messageBoxW         = user32.NewProc("MessageBoxW")
	postMessageW        = user32.NewProc("PostMessageW")
	getMessageW         = user32.NewProc("GetMessageW")
	translateMessage    = user32.NewProc("TranslateMessage")
	dispatchMessageW    = user32.NewProc("DispatchMessageW")
	postQuitMessage     = user32.NewProc("PostQuitMessage")
	loadIconW           = user32.NewProc("LoadIconW")
	loadCursorW         = user32.NewProc("LoadCursorW")
	destroyWindow       = user32.NewProc("DestroyWindow")
	createPopupMenu     = user32.NewProc("CreatePopupMenu")
	appendMenuW         = user32.NewProc("AppendMenuW")
	trackPopupMenu      = user32.NewProc("TrackPopupMenu")
	destroyMenu         = user32.NewProc("DestroyMenu")
	getCursorPos        = user32.NewProc("GetCursorPos")
	setForegroundWindow = user32.NewProc("SetForegroundWindow")
	findWindowW         = user32.NewProc("FindWindowW")
	getModuleHandleW    = kernel32UI.NewProc("GetModuleHandleW")
	createMutexW        = kernel32UI.NewProc("CreateMutexW")
	closeHandle         = kernel32UI.NewProc("CloseHandle")
	shellNotifyIconW    = shell32.NewProc("Shell_NotifyIconW")
)

var (
	tray        notifyIconData
	statusText  uintptr
	loginButton uintptr
	bindButton  uintptr
	aiButton    uintptr
	uiModel     *app.Model
	uiRuntime   *app.Runtime
)

func Run(buildRuntime func() (*app.Runtime, error)) error {
	if buildRuntime == nil {
		return fmt.Errorf("Windows UI 缺少 Runtime 构造函数")
	}
	className := utf16Ptr("CtyunHelperWindow")
	mutex, alreadyRunning, err := acquireSingleInstance()
	if err != nil {
		return err
	}
	if mutex != 0 {
		defer closeHandle.Call(mutex)
	}
	if alreadyRunning {
		if hwnd, _, _ := findWindowW.Call(uintptr(unsafe.Pointer(className)), 0); hwnd != 0 {
			showWindow.Call(hwnd, swShow)
			setForegroundWindow.Call(hwnd)
		}
		return nil
	}

	// 单实例锁必须早于 DeviceCode、Profile、Scheduler 和 Clink 初始化。
	// 否则用户连续双击 exe 时，第二个进程可能在发现已有窗口前短暂启动
	// 第二条 Clink 会话，首次运行时甚至可能并发生成两份设备身份。
	runtime, err := buildRuntime()
	if err != nil {
		return err
	}
	if runtime == nil || runtime.Model() == nil {
		return fmt.Errorf("Windows UI 缺少 App Runtime")
	}
	uiRuntime = runtime
	model := runtime.Model()
	uiModel = model
	runtime.Start(context.Background())
	defer runtime.Stop()

	title := utf16Ptr("天翼云电脑助手")
	instance, _, _ := getModuleHandleW.Call(0)
	icon, _, _ := loadIconW.Call(0, idiApplication)
	cursor, _, _ := loadCursorW.Call(0, idcArrow)

	class := wndClassEx{
		Size:       uint32(unsafe.Sizeof(wndClassEx{})),
		Style:      csHRedraw | csVRedraw,
		WndProc:    syscall.NewCallback(windowProc),
		Instance:   instance,
		Icon:       icon,
		Cursor:     cursor,
		Background: colorWindow + 1,
		ClassName:  className,
		IconSmall:  icon,
	}
	if atom, _, callErr := registerClassExW.Call(uintptr(unsafe.Pointer(&class))); atom == 0 {
		return fmt.Errorf("注册 Windows 窗口类失败: %w", callErr)
	}

	hwnd, _, callErr := createWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		wsOverlappedWindow,
		cwUseDefault, cwUseDefault, 900, 620,
		0, 0, instance, 0,
	)
	if hwnd == 0 {
		return fmt.Errorf("创建主窗口失败: %w", callErr)
	}
	createStatusText(hwnd, instance)
	createActionButtons(hwnd, instance)
	updateStatusText(model.Snapshot())
	if err := addTrayIcon(hwnd, icon); err != nil {
		destroyWindow.Call(hwnd)
		return err
	}
	showWindow.Call(hwnd, swShow)
	updateWindow.Call(hwnd)

	events, unsubscribe := model.Events().Subscribe(16)
	defer unsubscribe()
	go func() {
		for event := range events {
			if event.Type == app.EventStateChanged {
				postMessageW.Call(hwnd, wmStateChanged, 0, 0)
			}
		}
	}()

	var message msg
	for {
		result, _, messageErr := getMessageW.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(result) == -1 {
			return fmt.Errorf("读取 Windows 消息失败: %w", messageErr)
		}
		if result == 0 {
			return nil
		}
		translateMessage.Call(uintptr(unsafe.Pointer(&message)))
		dispatchMessageW.Call(uintptr(unsafe.Pointer(&message)))
	}
}

func windowProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmClose:
		showWindow.Call(hwnd, swHide)
		return 0
	case wmStateChanged:
		if uiModel != nil {
			updateStatusText(uiModel.Snapshot())
		}
		return 0
	case wmTray:
		switch uint32(lParam) {
		case wmLButtonDblClk:
			showWindow.Call(hwnd, swShow)
			setForegroundWindow.Call(hwnd)
		case wmRButtonUp:
			showTrayMenu(hwnd)
		}
		return 0
	case wmCommand:
		switch uint16(wParam & 0xffff) {
		case menuOpen:
			showWindow.Call(hwnd, swShow)
			setForegroundWindow.Call(hwnd)
		case buttonLogin:
			openLoginWindow(hwnd)
		case buttonBind:
			if uiModel != nil && uiModel.Snapshot().Connection == app.ConnectionDeviceBind {
				openBindingWindow(hwnd)
			}
		case buttonAI, menuRunAI:
			startAITask(hwnd)
		case menuExit:
			removeTrayIcon()
			destroyWindow.Call(hwnd)
		}
		return 0
	case wmDestroy:
		removeTrayIcon()
		postQuitMessage.Call(0)
		return 0
	}
	result, _, _ := defWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return result
}

func createStatusText(hwnd, instance uintptr) {
	class := utf16Ptr("STATIC")
	text := utf16Ptr("")
	statusText, _, _ = createWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(class)),
		uintptr(unsafe.Pointer(text)),
		wsChild|wsVisible|ssLeft,
		24, 24, 820, 120,
		hwnd, 0, instance, 0,
	)
}

func createActionButtons(hwnd, instance uintptr) {
	class := utf16Ptr("BUTTON")
	loginText := utf16Ptr("登录 / 更换账号")
	loginButton, _, _ = createWindowExW.Call(
		0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(loginText)),
		wsChild|wsVisible|bsPushButton,
		24, 165, 150, 34,
		hwnd, buttonLogin, instance, 0,
	)
	bindText := utf16Ptr("完成设备绑定")
	bindButton, _, _ = createWindowExW.Call(
		0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(bindText)),
		wsChild|wsVisible|bsPushButton,
		186, 165, 150, 34,
		hwnd, buttonBind, instance, 0,
	)
	aiText := utf16Ptr("立即执行 AI 任务")
	aiButton, _, _ = createWindowExW.Call(
		0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(aiText)),
		wsChild|wsVisible|bsPushButton,
		348, 165, 150, 34,
		hwnd, buttonAI, instance, 0,
	)
}

func updateStatusText(state app.State) {
	if statusText == 0 {
		return
	}
	if loginButton != 0 {
		enabled := uintptr(1)
		if state.AITask.Running {
			enabled = 0
		}
		enableWindow.Call(loginButton, enabled)
	}
	if bindButton != 0 {
		enabled := uintptr(0)
		if state.Connection == app.ConnectionDeviceBind {
			enabled = 1
		}
		enableWindow.Call(bindButton, enabled)
	}
	if aiButton != 0 {
		enabled := uintptr(1)
		if state.AutomationPaused || state.Connection == app.ConnectionAuth || state.Connection == app.ConnectionDeviceBind || state.AITask.Running {
			enabled = 0
		}
		enableWindow.Call(aiButton, enabled)
	}
	connection := map[app.ConnectionState]string{
		app.ConnectionStopped:    "等待启动",
		app.ConnectionConnecting: "正在连接",
		app.ConnectionOnline:     "在线",
		app.ConnectionBackoff:    "等待重连",
		app.ConnectionPaused:     "已暂停",
		app.ConnectionAuth:       "需要登录",
		app.ConnectionDeviceBind: "需要绑定设备",
		app.ConnectionError:      "异常",
	}[state.Connection]
	if connection == "" {
		connection = string(state.Connection)
	}
	desktopName := state.DesktopName
	if desktopName == "" {
		desktopName = "未选择"
	}
	lastError := state.LastError
	if lastError == "" {
		lastError = "无"
	}
	aiStatus := "等待"
	if state.AutomationPaused {
		aiStatus = "已暂停"
	} else if state.AITask.Running {
		aiStatus = "运行中"
	}
	nextAI := "未安排"
	if !state.AITask.NextRun.IsZero() {
		nextAI = state.AITask.NextRun.Format("01-02 15:04")
	}
	aiError := state.AITask.LastError
	if aiError == "" {
		aiError = "无"
	}
	text := fmt.Sprintf(
		"连接状态：%s\r\n云电脑：%s\r\n当前积分：%d\r\nAI任务：%s，下次 %s\r\nAI最近异常：%s\r\n最近异常：%s",
		connection, desktopName, state.Points, aiStatus, nextAI, aiError, lastError,
	)
	ptr := utf16Ptr(text)
	setWindowTextW.Call(statusText, uintptr(unsafe.Pointer(ptr)))
}

func startAITask(_ uintptr) {
	if uiRuntime == nil {
		return
	}
	// Scheduler 会把运行状态和业务错误同步到 App Model；后台 goroutine 不直接
	// 调用 MessageBox，避免跨 Win32 UI 线程操作窗口。
	go func() { _ = uiRuntime.RunAITask() }()
}

func addTrayIcon(hwnd, icon uintptr) error {
	tray = notifyIconData{
		Size:            uint32(unsafe.Sizeof(notifyIconData{})),
		HWnd:            hwnd,
		ID:              1,
		Flags:           nifMessage | nifIcon | nifTip,
		CallbackMessage: wmTray,
		Icon:            icon,
	}
	copy(tray.Tip[:], syscall.StringToUTF16("天翼云电脑助手"))
	result, _, callErr := shellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&tray)))
	if result == 0 {
		return fmt.Errorf("创建系统托盘图标失败: %w", callErr)
	}
	return nil
}

func removeTrayIcon() {
	if tray.HWnd != 0 {
		shellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&tray)))
		tray = notifyIconData{}
	}
}

func showTrayMenu(hwnd uintptr) {
	menu, _, _ := createPopupMenu.Call()
	if menu == 0 {
		return
	}
	defer destroyMenu.Call(menu)
	openText := utf16Ptr("打开主界面")
	runAIText := utf16Ptr("立即执行 AI 任务")
	exitText := utf16Ptr("退出")
	appendMenuW.Call(menu, mfString, menuOpen, uintptr(unsafe.Pointer(openText)))
	appendMenuW.Call(menu, mfString, menuRunAI, uintptr(unsafe.Pointer(runAIText)))
	appendMenuW.Call(menu, mfSeparator, 0, 0)
	appendMenuW.Call(menu, mfString, menuExit, uintptr(unsafe.Pointer(exitText)))

	var cursor point
	getCursorPos.Call(uintptr(unsafe.Pointer(&cursor)))
	setForegroundWindow.Call(hwnd)
	command, _, _ := trackPopupMenu.Call(
		menu,
		tpmRightButton|tpmReturnCmd,
		uintptr(cursor.X), uintptr(cursor.Y), 0,
		hwnd, 0,
	)
	if command != 0 {
		windowProc(hwnd, wmCommand, command, 0)
	}
}

func acquireSingleInstance() (handle uintptr, alreadyRunning bool, err error) {
	name := utf16Ptr("Local\\CtyunHelper")
	handle, _, callErr := createMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		return 0, false, fmt.Errorf("创建单实例 Mutex 失败: %w", callErr)
	}
	if errors.Is(callErr, syscall.Errno(errorAlreadyExists)) {
		return handle, true, nil
	}
	return handle, false, nil
}

func utf16Ptr(value string) *uint16 {
	ptr, err := syscall.UTF16PtrFromString(value)
	if err != nil {
		panic(err)
	}
	return ptr
}

func ShowError(title, message string) {
	titlePtr := utf16Ptr(title)
	messagePtr := utf16Ptr(message)
	messageBoxW.Call(0, uintptr(unsafe.Pointer(messagePtr)), uintptr(unsafe.Pointer(titlePtr)), 0x00000010)
}
