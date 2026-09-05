//go:build windows

package winui

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/uvwt/CtyunHelper/internal/app"
)

const (
	csHRedraw            = 0x0002
	csVRedraw            = 0x0001
	cwUseDefault         = ^uintptr(0x7fffffff)
	wmDestroy            = 0x0002
	wmClose              = 0x0010
	wmSetFont            = 0x0030
	wmCommand            = 0x0111
	wmCtlColorStatic     = 0x0138
	wmLButtonDblClk      = 0x0203
	wmRButtonUp          = 0x0205
	wmApp                = 0x8000
	wmTray               = wmApp + 1
	wmStateChanged       = wmApp + 2
	wmLogoutCompleted    = wmApp + 3
	wsOverlappedWindow   = 0x00CF0000
	wsVisible            = 0x10000000
	wsChild              = 0x40000000
	ssLeft               = 0x00000000
	ssRight              = 0x00000002
	ssIcon               = 0x00000003
	ssEtchedHoriz        = 0x00000010
	bsPushButton         = 0x00000000
	bsGroupBox           = 0x00000007
	stmSetIcon           = 0x0170
	swHide               = 0
	swShow               = 5
	swpNoMove            = 0x0002
	swpNoZOrder          = 0x0004
	colorWindow          = 5
	idiApplication       = 32512
	idcArrow             = 32512
	defaultGUIFont       = 17
	mfString             = 0x0000
	mfSeparator          = 0x0800
	tpmRightButton       = 0x0002
	tpmReturnCmd         = 0x0100
	nimAdd               = 0x00000000
	nimDelete            = 0x00000002
	nifMessage           = 0x00000001
	nifIcon              = 0x00000002
	nifTip               = 0x00000004
	menuOpen             = 1001
	menuRunAI            = 1002
	menuRefreshPoints    = 1003
	menuRunRedeem        = 1004
	menuRedeemSettings   = 1005
	menuSettings         = 1006
	menuLogs             = 1007
	menuLogout           = 1008
	menuExit             = 1009
	menuAbout            = 1010
	buttonLogin          = 1101
	buttonBind           = 1102
	buttonAI             = 1103
	buttonPoints         = 1104
	buttonRedeem         = 1105
	buttonRedeemSettings = 1106
	buttonSettings       = 1107
	buttonLogs           = 1108
	buttonLogout         = 1109
	buttonAbout          = 1110
	errorAlreadyExists   = 183
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
	user32               = syscall.NewLazyDLL("user32.dll")
	kernel32UI           = syscall.NewLazyDLL("kernel32.dll")
	gdi32UI              = syscall.NewLazyDLL("gdi32.dll")
	shell32              = syscall.NewLazyDLL("shell32.dll")
	shcore               = syscall.NewLazyDLL("shcore.dll")
	registerClassExW     = user32.NewProc("RegisterClassExW")
	createWindowExW      = user32.NewProc("CreateWindowExW")
	defWindowProcW       = user32.NewProc("DefWindowProcW")
	showWindow           = user32.NewProc("ShowWindow")
	updateWindow         = user32.NewProc("UpdateWindow")
	setWindowTextW       = user32.NewProc("SetWindowTextW")
	setWindowPosW        = user32.NewProc("SetWindowPos")
	enableWindow         = user32.NewProc("EnableWindow")
	messageBoxW          = user32.NewProc("MessageBoxW")
	postMessageW         = user32.NewProc("PostMessageW")
	getMessageW          = user32.NewProc("GetMessageW")
	translateMessage     = user32.NewProc("TranslateMessage")
	dispatchMessageW     = user32.NewProc("DispatchMessageW")
	postQuitMessage      = user32.NewProc("PostQuitMessage")
	loadIconW            = user32.NewProc("LoadIconW")
	loadCursorW          = user32.NewProc("LoadCursorW")
	destroyWindow        = user32.NewProc("DestroyWindow")
	createPopupMenu      = user32.NewProc("CreatePopupMenu")
	appendMenuW          = user32.NewProc("AppendMenuW")
	trackPopupMenu       = user32.NewProc("TrackPopupMenu")
	destroyMenu          = user32.NewProc("DestroyMenu")
	getCursorPos         = user32.NewProc("GetCursorPos")
	setForegroundWindow  = user32.NewProc("SetForegroundWindow")
	findWindowW          = user32.NewProc("FindWindowW")
	setProcessDPIAware   = user32.NewProc("SetProcessDPIAware")
	setProcessDPIContext = user32.NewProc("SetProcessDpiAwarenessContext")
	setProcessDPI        = shcore.NewProc("SetProcessDpiAwareness")
	getModuleHandleW     = kernel32UI.NewProc("GetModuleHandleW")
	createMutexW         = kernel32UI.NewProc("CreateMutexW")
	closeHandle          = kernel32UI.NewProc("CloseHandle")
	getStockObject       = gdi32UI.NewProc("GetStockObject")
	setTextColor         = gdi32UI.NewProc("SetTextColor")
	setBkMode            = gdi32UI.NewProc("SetBkMode")
	getSysColorBrush     = user32.NewProc("GetSysColorBrush")
	shellNotifyIconW     = shell32.NewProc("Shell_NotifyIconW")
)

type homeStatusControls struct {
	keepalive     uintptr
	desktop       uintptr
	points        uintptr
	pointsSync    uintptr
	redeem        uintptr
	redeemDesktop uintptr
	redeemProduct uintptr
	lastError     uintptr
	loginAITask   uintptr
	usageTask     uintptr
	aiPointsTask  uintptr
}

const (
	statusColorDefault uint32 = 0x00202020
	statusColorSuccess uint32 = 0x00006400
	statusColorWarning uint32 = 0x000080E0
	statusColorError   uint32 = 0x000000C0
	statusColorMuted   uint32 = 0x00707070
	statusColorInfo    uint32 = 0x00A06000
)

var (
	tray                 notifyIconData
	homeStatus           homeStatusControls
	homeStatusColors     = make(map[uintptr]uint32)
	loginButton          uintptr
	bindButton           uintptr
	aiButton             uintptr
	pointsButton         uintptr
	redeemButton         uintptr
	redeemSettingsButton uintptr
	settingsButton       uintptr
	logsButton           uintptr
	logoutButton         uintptr
	aboutButton          uintptr
	uiModel              *app.Model
	uiRuntime            *app.Runtime
	logoutMu             sync.Mutex
	logoutRunning        bool
	logoutErr            error
)

func Run(buildRuntime func() (*app.Runtime, error), options RunOptions) error {
	if buildRuntime == nil {
		return fmt.Errorf("Windows UI 缺少 Runtime 构造函数")
	}
	// Win32 窗口和消息队列属于创建它们的 OS 线程。Go goroutine 默认可以
	// 在线程之间迁移；如果创建窗口后迁到另一条线程再调用 GetMessage，原
	// 窗口线程就可能无人泵消息，最终被 Windows 判定为“未响应”。整个 UI
	// 生命周期固定在同一 OS 线程，后台网络任务仍使用普通 goroutine。
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	className := utf16Ptr("CtyunHelperWindow")
	mutex, alreadyRunning, err := acquireSingleInstance()
	if err != nil {
		return err
	}
	if mutex != 0 {
		defer closeHandle.Call(mutex)
	}
	if alreadyRunning {
		if !options.StartHidden {
			if hwnd := findExistingMainWindow(className, 2*time.Second); hwnd != 0 {
				showWindow.Call(hwnd, swShow)
				setForegroundWindow.Call(hwnd)
			}
		}
		return nil
	}
	enableDPIAwareness()

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
	fallbackIcon, _, _ := loadIconW.Call(0, idiApplication)
	largeIcon, smallIcon := fallbackIcon, fallbackIcon
	if err := loadBundledAppIcons(); err == nil {
		largeIcon, smallIcon = appIconLarge, appIconSmall
		defer releaseBundledAppIcons()
	}
	cursor, _, _ := loadCursorW.Call(0, idcArrow)

	class := wndClassEx{
		Size:       uint32(unsafe.Sizeof(wndClassEx{})),
		Style:      csHRedraw | csVRedraw,
		WndProc:    syscall.NewCallback(windowProc),
		Instance:   instance,
		Icon:       largeIcon,
		Cursor:     cursor,
		Background: colorWindow + 1,
		ClassName:  className,
		IconSmall:  smallIcon,
	}
	if atom, _, callErr := registerClassExW.Call(uintptr(unsafe.Pointer(&class))); atom == 0 {
		return fmt.Errorf("注册 Windows 窗口类失败: %w", callErr)
	}

	hwnd, _, callErr := createWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		wsOverlappedWindow,
		cwUseDefault, cwUseDefault, 920, 530,
		0, 0, instance, 0,
	)
	if hwnd == 0 {
		return fmt.Errorf("创建主窗口失败: %w", callErr)
	}
	createStatusText(hwnd, instance)
	createActionButtons(hwnd, instance)
	updateStatusText(model.Snapshot())
	if err := addTrayIcon(hwnd, smallIcon); err != nil {
		destroyWindow.Call(hwnd)
		return err
	}
	if !options.StartHidden {
		showWindow.Call(hwnd, swShow)
		updateWindow.Call(hwnd)
	}

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
	case wmLogoutCompleted:
		logoutMu.Lock()
		err := logoutErr
		logoutErr = nil
		logoutRunning = false
		logoutMu.Unlock()
		if uiModel != nil {
			updateStatusText(uiModel.Snapshot())
		}
		if err != nil {
			showMessage(hwnd, "退出账号", err.Error(), mbIconError)
		} else {
			showMessage(hwnd, "退出账号", "已退出账号并清除本地登录凭据。", mbInformation)
		}
		return 0
	case wmCtlColorStatic:
		if color, ok := homeStatusColors[lParam]; ok {
			setTextColor.Call(wParam, uintptr(color))
			setBkMode.Call(wParam, 1) // TRANSPARENT
			brush, _, _ := getSysColorBrush.Call(colorWindow)
			return brush
		}
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
		case buttonPoints, menuRefreshPoints:
			startPointsTask(hwnd)
		case buttonRedeem, menuRunRedeem:
			startRedeemTask(hwnd)
		case buttonRedeemSettings, menuRedeemSettings:
			openRedeemSettingsWindow(hwnd)
		case buttonSettings, menuSettings:
			openSettingsWindow(hwnd)
		case buttonLogs, menuLogs:
			openLogsWindow(hwnd)
		case buttonAbout, menuAbout:
			openAboutWindow(hwnd)
		case buttonLogout, menuLogout:
			startLogout(hwnd)
		case menuExit:
			closeAuxiliaryWindows()
			removeTrayIcon()
			destroyWindow.Call(hwnd)
		}
		return 0
	case wmDestroy:
		closeAuxiliaryWindows()
		removeTrayIcon()
		postQuitMessage.Call(0)
		return 0
	}
	result, _, _ := defWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return result
}

func createStatusText(hwnd, instance uintptr) {
	// 首页按优先级分成两层：核心运行状态在上，天翼服务端积分任务独立成组。
	// “积分同步”只是本地只读刷新 Job，不再和服务端积分任务混为一谈。
	createControl("BUTTON", "核心状态", wsChild|wsVisible|bsGroupBox, 24, 20, 840, 164, hwnd, 0, instance)

	createLabel(hwnd, instance, "保活状态", 46, 48, 96, 24)
	homeStatus.keepalive = createLabel(hwnd, instance, "", 146, 48, 260, 24)
	createLabel(hwnd, instance, "当前云电脑", 46, 82, 96, 24)
	homeStatus.desktop = createLabel(hwnd, instance, "", 146, 82, 260, 24)
	createLabel(hwnd, instance, "当前积分", 46, 116, 96, 24)
	homeStatus.points = createLabel(hwnd, instance, "", 146, 116, 260, 24)
	createLabel(hwnd, instance, "积分同步", 46, 150, 96, 24)
	homeStatus.pointsSync = createLabel(hwnd, instance, "", 146, 150, 260, 24)

	createLabel(hwnd, instance, "自动兑换", 430, 48, 96, 24)
	homeStatus.redeem = createLabel(hwnd, instance, "", 530, 48, 304, 24)
	createLabel(hwnd, instance, "配置云电脑", 430, 82, 96, 24)
	homeStatus.redeemDesktop = createLabel(hwnd, instance, "", 530, 82, 304, 24)
	createLabel(hwnd, instance, "兑换商品", 430, 116, 96, 24)
	homeStatus.redeemProduct = createLabel(hwnd, instance, "", 530, 116, 304, 24)
	createLabel(hwnd, instance, "最近异常", 430, 150, 96, 24)
	homeStatus.lastError = createLabel(hwnd, instance, "", 530, 150, 304, 24)

	createControl("BUTTON", "积分任务", wsChild|wsVisible|bsGroupBox, 24, 194, 840, 82, hwnd, 0, instance)
	createLabel(hwnd, instance, "登录AI云电脑", 46, 224, 100, 24)
	homeStatus.loginAITask = createLabel(hwnd, instance, "", 150, 224, 118, 24)
	createLabel(hwnd, instance, "使用1小时", 308, 224, 80, 24)
	homeStatus.usageTask = createLabel(hwnd, instance, "", 392, 224, 120, 24)
	createLabel(hwnd, instance, "与AI对话1次", 552, 224, 100, 24)
	homeStatus.aiPointsTask = createLabel(hwnd, instance, "", 656, 224, 178, 24)
}

func createActionButtons(hwnd, instance uintptr) {
	createControl("BUTTON", "快捷操作", wsChild|wsVisible|bsGroupBox, 24, 294, 840, 150, hwnd, 0, instance)

	loginButton = createControl("BUTTON", "账号登录", wsChild|wsVisible|wsTabStop|bsPushButton, 46, 324, 174, 38, hwnd, buttonLogin, instance)
	bindButton = createControl("BUTTON", "设备绑定", wsChild|wsVisible|wsTabStop|bsPushButton, 232, 324, 154, 38, hwnd, buttonBind, instance)
	aiButton = createControl("BUTTON", "执行 AI 任务", wsChild|wsVisible|wsTabStop|bsPushButton, 398, 324, 164, 38, hwnd, buttonAI, instance)
	pointsButton = createControl("BUTTON", "刷新积分", wsChild|wsVisible|wsTabStop|bsPushButton, 574, 324, 134, 38, hwnd, buttonPoints, instance)
	redeemButton = createControl("BUTTON", "检查兑换", wsChild|wsVisible|wsTabStop|bsPushButton, 720, 324, 122, 38, hwnd, buttonRedeem, instance)

	settingsButton = createControl("BUTTON", "设置", wsChild|wsVisible|wsTabStop|bsPushButton, 46, 380, 122, 36, hwnd, buttonSettings, instance)
	logsButton = createControl("BUTTON", "日志", wsChild|wsVisible|wsTabStop|bsPushButton, 180, 380, 122, 36, hwnd, buttonLogs, instance)
	logoutButton = createControl("BUTTON", "退出账号", wsChild|wsVisible|wsTabStop|bsPushButton, 314, 380, 136, 36, hwnd, buttonLogout, instance)
	aboutButton = createControl("BUTTON", "关于", wsChild|wsVisible|wsTabStop|bsPushButton, 586, 380, 122, 36, hwnd, buttonAbout, instance)
	redeemSettingsButton = createControl("BUTTON", "兑换设置", wsChild|wsVisible|wsTabStop|bsPushButton, 720, 380, 122, 36, hwnd, buttonRedeemSettings, instance)
}

func applySystemFont(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	font, _, _ := getStockObject.Call(defaultGUIFont)
	if font != 0 {
		// 仅在当前 UI 线程给本进程控件设置系统 GUI 字体，不涉及跨进程消息。
		sendMessageW.Call(hwnd, wmSetFont, font, 1)
	}
}

func createAppIconControl(parent, instance, icon, x, y, width, height uintptr) uintptr {
	control := createControl("STATIC", "", wsChild|wsVisible|ssIcon, x, y, width, height, parent, 0, instance)
	if control != 0 && icon != 0 {
		sendMessageW.Call(control, stmSetIcon, icon, 0)
	}
	return control
}

func updateStatusText(state app.State) {
	if homeStatus.keepalive == 0 {
		return
	}
	jobsRunning := state.AITask.Running || state.PointsTask.Running || state.RedeemTask.Running
	if loginButton != 0 {
		if state.Account == "" {
			setControlText(loginButton, "账号登录")
		} else {
			setControlText(loginButton, "更换账号")
		}
		enabled := uintptr(1)
		if jobsRunning {
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
	if pointsButton != 0 {
		enabled := uintptr(1)
		if state.Connection == app.ConnectionAuth || state.Connection == app.ConnectionDeviceBind || state.PointsTask.Running || state.RedeemTask.Running {
			enabled = 0
		}
		enableWindow.Call(pointsButton, enabled)
	}
	if redeemButton != 0 {
		enabled := uintptr(1)
		if !state.RedeemEnabled || state.AutomationPaused || state.Connection == app.ConnectionAuth || state.Connection == app.ConnectionDeviceBind || state.PointsTask.Running || state.RedeemTask.Running {
			enabled = 0
		}
		enableWindow.Call(redeemButton, enabled)
	}
	if logoutButton != 0 {
		enabled := uintptr(0)
		if state.Account != "" && !jobsRunning {
			logoutMu.Lock()
			running := logoutRunning
			logoutMu.Unlock()
			if !running {
				enabled = 1
			}
		}
		enableWindow.Call(logoutButton, enabled)
	}

	keepalive, keepaliveColor := connectionStatusText(state.Connection)
	setHomeStatusState(homeStatus.keepalive, keepalive, keepaliveColor)

	desktopName := state.DesktopName
	desktopColor := statusColorInfo
	if desktopName == "" {
		desktopName = "未选择"
		desktopColor = statusColorMuted
	}
	setHomeStatusValue(homeStatus.desktop, desktopName, desktopColor)
	setHomeStatusValue(homeStatus.points, fmt.Sprintf("%d", state.Points), statusColorInfo)

	pointsSync, pointsSyncColor := jobStatusText(state.PointsTask, "刷新中", false)
	setHomeStatusState(homeStatus.pointsSync, pointsSync, pointsSyncColor)

	redeemStatus, redeemColor := redeemHomeStatusText(state)
	setHomeStatusState(homeStatus.redeem, redeemStatus, redeemColor)

	configuredDesktop := state.RedeemDesktopName
	configuredDesktopColor := statusColorInfo
	if configuredDesktop == "" {
		configuredDesktop = "未配置"
		configuredDesktopColor = statusColorMuted
	}
	setHomeStatusValue(homeStatus.redeemDesktop, configuredDesktop, configuredDesktopColor)

	configuredProduct := state.RedeemProductName
	configuredProductColor := statusColorInfo
	if configuredProduct == "" {
		configuredProduct = "未配置"
		configuredProductColor = statusColorMuted
	} else if state.RedeemCostPoints > 0 {
		configuredProduct = fmt.Sprintf("%s（%d 积分）", configuredProduct, state.RedeemCostPoints)
	}
	setHomeStatusValue(homeStatus.redeemProduct, configuredProduct, configuredProductColor)

	lastError := state.LastError
	lastErrorColor := statusColorError
	if lastError == "" {
		lastError = "无"
		lastErrorColor = statusColorSuccess
	}
	setHomeStatusState(homeStatus.lastError, lastError, lastErrorColor)

	loginAI, loginAIColor := pointsTaskStatusText(state.LoginAITask)
	setHomeStatusState(homeStatus.loginAITask, loginAI, loginAIColor)
	usage, usageColor := pointsTaskStatusText(state.UsageTask)
	setHomeStatusState(homeStatus.usageTask, usage, usageColor)
	aiPoints, aiPointsColor := pointsTaskStatusText(state.AIPointsTask)
	setHomeStatusState(homeStatus.aiPointsTask, aiPoints, aiPointsColor)
}

func setHomeStatusState(control uintptr, text string, color uint32) {
	setHomeStatusValue(control, homeStatusIndicatorText(text), color)
}

func homeStatusIndicatorText(text string) string {
	return "● " + text
}

func setHomeStatusValue(control uintptr, text string, color uint32) {
	if control == 0 {
		return
	}
	homeStatusColors[control] = color
	setControlText(control, text)
}

func connectionStatusText(state app.ConnectionState) (string, uint32) {
	switch state {
	case app.ConnectionOnline:
		return "在线", statusColorSuccess
	case app.ConnectionConnecting:
		return "正在连接", statusColorWarning
	case app.ConnectionBackoff:
		return "等待重连", statusColorWarning
	case app.ConnectionPaused:
		return "已暂停", statusColorWarning
	case app.ConnectionAuth:
		return "需要登录", statusColorError
	case app.ConnectionDeviceBind:
		return "需要绑定设备", statusColorWarning
	case app.ConnectionError:
		return "异常", statusColorError
	case app.ConnectionStopped:
		return "等待启动", statusColorMuted
	default:
		return string(state), statusColorDefault
	}
}

func pointsTaskStatusText(status app.PointsTaskStatus) (string, uint32) {
	if !status.Found {
		return "等待刷新", statusColorMuted
	}
	if status.Status == 2 {
		return "已完成", statusColorSuccess
	}
	if status.Progress > 0 {
		return fmt.Sprintf("进行中（进度 %d）", status.Progress), statusColorWarning
	}
	return "待完成", statusColorWarning
}

func jobStatusText(status app.JobStatus, runningText string, paused bool) (string, uint32) {
	if paused {
		return "已暂停", statusColorWarning
	}
	if status.Running {
		return runningText, statusColorWarning
	}
	if status.LastError != "" {
		return "异常：" + status.LastError, statusColorError
	}
	if !status.LastRun.IsZero() {
		return "已完成（" + status.LastRun.Format("01-02 15:04") + "）", statusColorSuccess
	}
	return "等待首次执行", statusColorMuted
}

func redeemHomeStatusText(state app.State) (string, uint32) {
	if state.RedeemTask.Running {
		return "运行中", statusColorWarning
	}
	if state.RedeemTask.LastError != "" {
		return "异常：" + state.RedeemTask.LastError, statusColorError
	}
	if !state.RedeemEnabled {
		if state.RedeemSummary != "" {
			if state.RedeemSummary == "未启用" {
				return "未启用", statusColorMuted
			}
			return state.RedeemSummary, statusColorWarning
		}
		return "未启用", statusColorMuted
	}
	if state.AutomationPaused {
		return "已暂停", statusColorWarning
	}
	if !state.RedeemTask.LastRun.IsZero() {
		text := "已完成（" + state.RedeemTask.LastRun.Format("01-02 15:04") + "）"
		if state.RedeemSummary != "" && state.RedeemSummary != "等待兑换计划" {
			text += "；" + state.RedeemSummary
		}
		return text, statusColorSuccess
	}
	return "等待执行", statusColorInfo
}

func startAITask(_ uintptr) {
	if uiRuntime == nil {
		return
	}
	// Scheduler 会把运行状态和业务错误同步到 App Model；后台 goroutine 不直接
	// 调用 MessageBox，避免跨 Win32 UI 线程操作窗口。
	go func() { _ = uiRuntime.RunAITask() }()
}

func startPointsTask(_ uintptr) {
	if uiRuntime == nil {
		return
	}
	go func() { _ = uiRuntime.RunPointsTask() }()
}

func startRedeemTask(_ uintptr) {
	if uiRuntime == nil {
		return
	}
	go func() { _ = uiRuntime.RunRedeemTask() }()
}

func startLogout(owner uintptr) {
	if uiRuntime == nil || uiModel == nil || uiModel.Snapshot().Account == "" {
		return
	}
	logoutMu.Lock()
	if logoutRunning {
		logoutMu.Unlock()
		return
	}
	logoutMu.Unlock()
	if !confirmLogout(owner) {
		return
	}

	logoutMu.Lock()
	if logoutRunning {
		logoutMu.Unlock()
		return
	}
	logoutRunning = true
	logoutMu.Unlock()
	if logoutButton != 0 {
		enableWindow.Call(logoutButton, 0)
	}
	go func() {
		err := uiRuntime.Logout()
		logoutMu.Lock()
		logoutErr = err
		logoutMu.Unlock()
		postMessageW.Call(owner, wmLogoutCompleted, 0, 0)
	}()
}

func confirmLogout(owner uintptr) bool {
	title := utf16Ptr("退出账号")
	message := utf16Ptr("退出后会停止当前云电脑会话，并删除本机保存的账号密码和登录 Profile。\n\n确定退出账号吗？")
	result, _, _ := messageBoxW.Call(
		owner,
		uintptr(unsafe.Pointer(message)),
		uintptr(unsafe.Pointer(title)),
		mbYesNo|mbIconWarning,
	)
	return result == idYes
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

// enableDPIAwareness 必须在创建任何窗口前调用。Windows 10 1703+ 优先
// Per-Monitor V2；较旧系统依次降级为 Per-Monitor 和 System DPI aware。
// DPI API 不可用或宿主已设置 DPI 模式都不是启动失败条件。
func enableDPIAwareness() {
	if err := setProcessDPIContext.Find(); err == nil {
		const dpiAwarenessContextPerMonitorV2 = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
		if result, _, _ := setProcessDPIContext.Call(dpiAwarenessContextPerMonitorV2); result != 0 {
			return
		}
	}
	if err := setProcessDPI.Find(); err == nil {
		const processPerMonitorDPIAware = 2
		if result, _, _ := setProcessDPI.Call(processPerMonitorDPIAware); int32(result) == 0 {
			return
		}
	}
	if err := setProcessDPIAware.Find(); err == nil {
		setProcessDPIAware.Call()
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
	refreshPointsText := utf16Ptr("刷新积分")
	runRedeemText := utf16Ptr("检查 / 执行兑换")
	redeemSettingsText := utf16Ptr("兑换设置")
	settingsText := utf16Ptr("设置")
	logsText := utf16Ptr("日志")
	aboutText := utf16Ptr("关于")
	logoutText := utf16Ptr("退出账号")
	exitText := utf16Ptr("退出")
	appendMenuW.Call(menu, mfString, menuOpen, uintptr(unsafe.Pointer(openText)))
	appendMenuW.Call(menu, mfString, menuRunAI, uintptr(unsafe.Pointer(runAIText)))
	appendMenuW.Call(menu, mfString, menuRefreshPoints, uintptr(unsafe.Pointer(refreshPointsText)))
	if uiModel != nil && uiModel.Snapshot().RedeemEnabled {
		appendMenuW.Call(menu, mfString, menuRunRedeem, uintptr(unsafe.Pointer(runRedeemText)))
	}
	appendMenuW.Call(menu, mfString, menuRedeemSettings, uintptr(unsafe.Pointer(redeemSettingsText)))
	appendMenuW.Call(menu, mfString, menuSettings, uintptr(unsafe.Pointer(settingsText)))
	appendMenuW.Call(menu, mfString, menuLogs, uintptr(unsafe.Pointer(logsText)))
	appendMenuW.Call(menu, mfString, menuAbout, uintptr(unsafe.Pointer(aboutText)))
	if uiModel != nil && uiModel.Snapshot().Account != "" {
		appendMenuW.Call(menu, mfString, menuLogout, uintptr(unsafe.Pointer(logoutText)))
	}
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

func findExistingMainWindow(className *uint16, timeout time.Duration) uintptr {
	deadline := time.Now().Add(timeout)
	for {
		hwnd, _, _ := findWindowW.Call(uintptr(unsafe.Pointer(className)), 0)
		if hwnd != 0 {
			return hwnd
		}
		if timeout <= 0 || !time.Now().Before(deadline) {
			return 0
		}
		time.Sleep(50 * time.Millisecond)
	}
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
