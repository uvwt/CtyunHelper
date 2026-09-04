//go:build windows

package winui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/uvwt/CtyunHelper/internal/ctyun/auth"
)

const (
	wsBorder       = 0x00800000
	wsTabStop      = 0x00010000
	esAutoHScroll  = 0x0080
	esPassword     = 0x0020
	ssBitmap       = 0x0000000E
	mbIconError    = 0x00000010
	mbInformation  = 0x00000040
	loginRefresh   = 2101
	loginSubmit    = 2102
	bindingSendSMS = 2201
	bindingSubmit  = 2202

	wmLoginCaptchaReady   = wmApp + 10
	wmLoginCompleted      = wmApp + 11
	wmBindingCaptchaReady = wmApp + 12
	wmBindingSMSReady     = wmApp + 13
	wmBindingCompleted    = wmApp + 14
)

var (
	getWindowTextLengthW = user32.NewProc("GetWindowTextLengthW")
	getWindowTextW       = user32.NewProc("GetWindowTextW")

	authClassOnce sync.Once
	authClassErr  error
	dialogMu      sync.Mutex
	loginDialog   *loginDialogState
	bindingDialog *bindingDialogState
)

type loginDialogState struct {
	mu           sync.Mutex
	hwnd         uintptr
	owner        uintptr
	accountEdit  uintptr
	passwordEdit uintptr
	captchaEdit  uintptr
	captchaView  uintptr
	challenge    auth.LoginChallenge
	challengeFor string
	bitmap       uintptr
	resultErr    error
	bonded       bool
	busy         bool
}

type bindingDialogState struct {
	mu          sync.Mutex
	hwnd        uintptr
	owner       uintptr
	mobileText  uintptr
	captchaEdit uintptr
	smsEdit     uintptr
	captchaView uintptr
	challenge   auth.DeviceBindingChallenge
	smsKey      string
	bitmap      uintptr
	resultErr   error
	busy        bool
}

func beginDialogWork(mu *sync.Mutex, busy *bool) bool {
	mu.Lock()
	defer mu.Unlock()
	if *busy {
		return false
	}
	*busy = true
	return true
}

func endDialogWork(mu *sync.Mutex, busy *bool) {
	mu.Lock()
	*busy = false
	mu.Unlock()
}

func openLoginWindow(owner uintptr) {
	if uiRuntime == nil {
		return
	}
	dialogMu.Lock()
	if loginDialog != nil && loginDialog.hwnd != 0 {
		hwnd := loginDialog.hwnd
		dialogMu.Unlock()
		showWindow.Call(hwnd, swShow)
		setForegroundWindow.Call(hwnd)
		return
	}
	dialogMu.Unlock()
	if err := ensureAuthWindowClasses(); err != nil {
		showMessage(owner, "登录", err.Error(), mbIconError)
		return
	}
	instance, _, _ := getModuleHandleW.Call(0)
	className := utf16Ptr("CtyunHelperLoginWindow")
	title := utf16Ptr("登录天翼云电脑")
	hwnd, _, callErr := createWindowExW.Call(
		0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)), wsOverlappedWindow,
		cwUseDefault, cwUseDefault, 520, 410,
		owner, 0, instance, 0,
	)
	if hwnd == 0 {
		showMessage(owner, "登录", fmt.Sprintf("创建登录窗口失败: %v", callErr), mbIconError)
		return
	}
	state := &loginDialogState{hwnd: hwnd, owner: owner}
	createLoginControls(state, instance)
	dialogMu.Lock()
	loginDialog = state
	dialogMu.Unlock()
	if account, password, err := uiRuntime.LoadStoredLogin(); err == nil {
		setControlText(state.accountEdit, account)
		setControlText(state.passwordEdit, password)
	} else if uiModel != nil {
		setControlText(state.accountEdit, uiModel.Snapshot().Account)
	}
	showWindow.Call(hwnd, swShow)
	updateWindow.Call(hwnd)
}

func createLoginControls(state *loginDialogState, instance uintptr) {
	createLabel(state.hwnd, instance, "账号", 24, 24, 80, 24)
	state.accountEdit = createControl("EDIT", "", wsChild|wsVisible|wsBorder|wsTabStop|esAutoHScroll, 110, 20, 350, 28, state.hwnd, 0, instance)
	createLabel(state.hwnd, instance, "密码", 24, 66, 80, 24)
	state.passwordEdit = createControl("EDIT", "", wsChild|wsVisible|wsBorder|wsTabStop|esAutoHScroll|esPassword, 110, 62, 350, 28, state.hwnd, 0, instance)
	createLabel(state.hwnd, instance, "图形验证码", 24, 110, 80, 24)
	state.captchaView = createControl("STATIC", "", wsChild|wsVisible|ssBitmap, 110, 104, 120, 56, state.hwnd, 0, instance)
	createControl("BUTTON", "获取 / 刷新", wsChild|wsVisible|wsTabStop|bsPushButton, 250, 111, 110, 32, state.hwnd, loginRefresh, instance)
	createLabel(state.hwnd, instance, "验证码", 24, 180, 80, 24)
	state.captchaEdit = createControl("EDIT", "", wsChild|wsVisible|wsBorder|wsTabStop|esAutoHScroll, 110, 176, 160, 28, state.hwnd, 0, instance)
	createControl("BUTTON", "登录", wsChild|wsVisible|wsTabStop|bsPushButton, 110, 230, 120, 36, state.hwnd, loginSubmit, instance)
	createLabel(state.hwnd, instance, "密码成功登录后只保存到 Windows Credential Manager。", 24, 300, 440, 36)
}

func loginWindowProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	dialogMu.Lock()
	state := loginDialog
	dialogMu.Unlock()
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
		case loginRefresh:
			startLoginCaptcha(state)
		case loginSubmit:
			startLoginSubmit(state)
		}
		return 0
	case wmLoginCaptchaReady:
		state.mu.Lock()
		err := state.resultErr
		state.resultErr = nil
		captcha := append([]byte(nil), state.challenge.Captcha...)
		clear(state.challenge.Captcha)
		state.challenge.Captcha = nil
		state.busy = false
		state.mu.Unlock()
		if err != nil {
			showMessage(hwnd, "验证码", err.Error(), mbIconError)
			return 0
		}
		bitmap, err := setPNGOnStatic(state.captchaView, captcha)
		clear(captcha)
		if err != nil {
			showMessage(hwnd, "验证码", err.Error(), mbIconError)
			return 0
		}
		state.mu.Lock()
		state.bitmap = bitmap
		state.mu.Unlock()
		return 0
	case wmLoginCompleted:
		state.mu.Lock()
		err := state.resultErr
		bonded := state.bonded
		state.resultErr = nil
		state.busy = false
		state.mu.Unlock()
		if err != nil {
			showMessage(hwnd, "登录失败", err.Error(), mbIconError)
			return 0
		}
		owner := state.owner
		destroyWindow.Call(hwnd)
		if !bonded {
			openBindingWindow(owner)
		}
		return 0
	case wmDestroy:
		state.mu.Lock()
		if state.bitmap != 0 {
			deleteObject.Call(state.bitmap)
			state.bitmap = 0
		}
		state.mu.Unlock()
		dialogMu.Lock()
		if loginDialog == state {
			loginDialog = nil
		}
		dialogMu.Unlock()
		return 0
	}
	result, _, _ := defWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return result
}

func startLoginCaptcha(state *loginDialogState) {
	if !beginDialogWork(&state.mu, &state.busy) {
		showMessage(state.hwnd, "登录", "当前操作尚未完成。", mbInformation)
		return
	}
	account := strings.TrimSpace(readControlText(state.accountEdit))
	if account == "" {
		endDialogWork(&state.mu, &state.busy)
		showMessage(state.hwnd, "登录", "请输入账号。", mbIconError)
		return
	}
	state.mu.Lock()
	clear(state.challenge.Captcha)
	state.challenge = auth.LoginChallenge{}
	state.challengeFor = ""
	state.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		challenge, err := uiRuntime.BeginLogin(ctx, account)
		state.mu.Lock()
		state.resultErr = err
		if err == nil {
			state.challenge = challenge
			state.challengeFor = account
		}
		state.mu.Unlock()
		postMessageW.Call(state.hwnd, wmLoginCaptchaReady, 0, 0)
	}()
}

func startLoginSubmit(state *loginDialogState) {
	if !beginDialogWork(&state.mu, &state.busy) {
		showMessage(state.hwnd, "登录", "当前操作尚未完成。", mbInformation)
		return
	}
	account := strings.TrimSpace(readControlText(state.accountEdit))
	password := readControlText(state.passwordEdit)
	captchaCode := strings.TrimSpace(readControlText(state.captchaEdit))
	state.mu.Lock()
	challenge := state.challenge
	challengeFor := state.challengeFor
	state.mu.Unlock()
	if account == "" || password == "" || captchaCode == "" || challenge.ID == "" {
		endDialogWork(&state.mu, &state.busy)
		showMessage(state.hwnd, "登录", "请填写账号、密码和验证码，并先获取验证码。", mbIconError)
		return
	}
	if account != challengeFor {
		endDialogWork(&state.mu, &state.busy)
		showMessage(state.hwnd, "登录", "账号已变化，请重新获取图形验证码。", mbIconError)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		profile, err := uiRuntime.CompleteLogin(ctx, account, password, captchaCode, challenge)
		state.mu.Lock()
		state.resultErr = err
		state.bonded = err == nil && profile.BondedDevice
		state.mu.Unlock()
		postMessageW.Call(state.hwnd, wmLoginCompleted, 0, 0)
	}()
}

func openBindingWindow(owner uintptr) {
	if uiRuntime == nil {
		return
	}
	dialogMu.Lock()
	if bindingDialog != nil && bindingDialog.hwnd != 0 {
		hwnd := bindingDialog.hwnd
		dialogMu.Unlock()
		showWindow.Call(hwnd, swShow)
		setForegroundWindow.Call(hwnd)
		return
	}
	dialogMu.Unlock()
	if err := ensureAuthWindowClasses(); err != nil {
		showMessage(owner, "设备绑定", err.Error(), mbIconError)
		return
	}
	instance, _, _ := getModuleHandleW.Call(0)
	className := utf16Ptr("CtyunHelperBindingWindow")
	title := utf16Ptr("绑定当前 Windows 设备")
	hwnd, _, callErr := createWindowExW.Call(
		0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)), wsOverlappedWindow,
		cwUseDefault, cwUseDefault, 520, 450,
		owner, 0, instance, 0,
	)
	if hwnd == 0 {
		showMessage(owner, "设备绑定", fmt.Sprintf("创建设备绑定窗口失败: %v", callErr), mbIconError)
		return
	}
	state := &bindingDialogState{hwnd: hwnd, owner: owner}
	createBindingControls(state, instance)
	dialogMu.Lock()
	bindingDialog = state
	dialogMu.Unlock()
	showWindow.Call(hwnd, swShow)
	updateWindow.Call(hwnd)
	startBindingCaptcha(state)
}

func createBindingControls(state *bindingDialogState, instance uintptr) {
	state.mobileText = createLabel(state.hwnd, instance, "正在获取设备验证码...", 24, 20, 440, 24)
	createLabel(state.hwnd, instance, "图形验证码", 24, 62, 80, 24)
	state.captchaView = createControl("STATIC", "", wsChild|wsVisible|ssBitmap, 110, 56, 120, 56, state.hwnd, 0, instance)
	createLabel(state.hwnd, instance, "验证码", 24, 132, 80, 24)
	state.captchaEdit = createControl("EDIT", "", wsChild|wsVisible|wsBorder|wsTabStop|esAutoHScroll, 110, 128, 160, 28, state.hwnd, 0, instance)
	createControl("BUTTON", "发送短信", wsChild|wsVisible|wsTabStop|bsPushButton, 290, 126, 110, 32, state.hwnd, bindingSendSMS, instance)
	createLabel(state.hwnd, instance, "短信验证码", 24, 184, 80, 24)
	state.smsEdit = createControl("EDIT", "", wsChild|wsVisible|wsBorder|wsTabStop|esAutoHScroll, 110, 180, 160, 28, state.hwnd, 0, instance)
	createControl("BUTTON", "完成绑定", wsChild|wsVisible|wsTabStop|bsPushButton, 110, 236, 120, 36, state.hwnd, bindingSubmit, instance)
	createLabel(state.hwnd, instance, "设备码首次生成后固定保存；本程序不会静默更换设备身份。", 24, 305, 440, 44)
}

func bindingWindowProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	dialogMu.Lock()
	state := bindingDialog
	dialogMu.Unlock()
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
		case bindingSendSMS:
			startSendBindingSMS(state)
		case bindingSubmit:
			startBindingSubmit(state)
		}
		return 0
	case wmBindingCaptchaReady:
		state.mu.Lock()
		err := state.resultErr
		state.resultErr = nil
		captcha := append([]byte(nil), state.challenge.Captcha...)
		clear(state.challenge.Captcha)
		state.challenge.Captcha = nil
		mobile := state.challenge.Mobile
		state.busy = false
		state.mu.Unlock()
		if err != nil {
			showMessage(hwnd, "设备绑定", err.Error(), mbIconError)
			return 0
		}
		setControlText(state.mobileText, "短信验证号码："+mobile)
		bitmap, err := setPNGOnStatic(state.captchaView, captcha)
		clear(captcha)
		if err != nil {
			showMessage(hwnd, "验证码", err.Error(), mbIconError)
			return 0
		}
		state.mu.Lock()
		state.bitmap = bitmap
		state.mu.Unlock()
		return 0
	case wmBindingSMSReady:
		state.mu.Lock()
		err := state.resultErr
		state.resultErr = nil
		state.busy = false
		state.mu.Unlock()
		if err != nil {
			showMessage(hwnd, "发送短信失败", err.Error(), mbIconError)
		} else {
			showMessage(hwnd, "设备绑定", "短信验证码已发送。", mbInformation)
		}
		return 0
	case wmBindingCompleted:
		state.mu.Lock()
		err := state.resultErr
		state.resultErr = nil
		state.busy = false
		state.mu.Unlock()
		if err != nil {
			showMessage(hwnd, "设备绑定失败", err.Error(), mbIconError)
			return 0
		}
		showMessage(hwnd, "设备绑定", "设备绑定成功，正在启动云电脑保活。", mbInformation)
		destroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		state.mu.Lock()
		if state.bitmap != 0 {
			deleteObject.Call(state.bitmap)
			state.bitmap = 0
		}
		state.mu.Unlock()
		dialogMu.Lock()
		if bindingDialog == state {
			bindingDialog = nil
		}
		dialogMu.Unlock()
		return 0
	}
	result, _, _ := defWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return result
}

func startBindingCaptcha(state *bindingDialogState) {
	if !beginDialogWork(&state.mu, &state.busy) {
		return
	}
	state.mu.Lock()
	clear(state.challenge.Captcha)
	state.challenge = auth.DeviceBindingChallenge{}
	state.smsKey = ""
	state.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		challenge, err := uiRuntime.BeginDeviceBinding(ctx)
		state.mu.Lock()
		state.resultErr = err
		if err == nil {
			state.challenge = challenge
		}
		state.mu.Unlock()
		postMessageW.Call(state.hwnd, wmBindingCaptchaReady, 0, 0)
	}()
}

func startSendBindingSMS(state *bindingDialogState) {
	if !beginDialogWork(&state.mu, &state.busy) {
		showMessage(state.hwnd, "设备绑定", "当前操作尚未完成。", mbInformation)
		return
	}
	captchaCode := strings.TrimSpace(readControlText(state.captchaEdit))
	state.mu.Lock()
	captchaKey := state.challenge.CaptchaKey
	state.mu.Unlock()
	if captchaCode == "" || captchaKey == "" {
		endDialogWork(&state.mu, &state.busy)
		showMessage(state.hwnd, "设备绑定", "请输入图形验证码。", mbIconError)
		return
	}
	state.mu.Lock()
	state.smsKey = ""
	state.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		smsKey, err := uiRuntime.SendDeviceSMS(ctx, captchaCode, captchaKey)
		state.mu.Lock()
		state.resultErr = err
		if err == nil {
			state.smsKey = smsKey
		}
		state.mu.Unlock()
		postMessageW.Call(state.hwnd, wmBindingSMSReady, 0, 0)
	}()
}

func startBindingSubmit(state *bindingDialogState) {
	if !beginDialogWork(&state.mu, &state.busy) {
		showMessage(state.hwnd, "设备绑定", "当前操作尚未完成。", mbInformation)
		return
	}
	smsCode := strings.TrimSpace(readControlText(state.smsEdit))
	state.mu.Lock()
	smsKey := state.smsKey
	state.mu.Unlock()
	if smsCode == "" || smsKey == "" {
		endDialogWork(&state.mu, &state.busy)
		showMessage(state.hwnd, "设备绑定", "请先发送短信并填写短信验证码。", mbIconError)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := uiRuntime.CompleteDeviceBinding(ctx, smsCode, smsKey)
		state.mu.Lock()
		state.resultErr = err
		state.mu.Unlock()
		postMessageW.Call(state.hwnd, wmBindingCompleted, 0, 0)
	}()
}

func ensureAuthWindowClasses() error {
	authClassOnce.Do(func() {
		if err := registerAuthWindowClass("CtyunHelperLoginWindow", loginWindowProc); err != nil {
			authClassErr = err
			return
		}
		if err := registerAuthWindowClass("CtyunHelperBindingWindow", bindingWindowProc); err != nil {
			authClassErr = err
		}
	})
	return authClassErr
}

func registerAuthWindowClass(name string, proc func(uintptr, uint32, uintptr, uintptr) uintptr) error {
	instance, _, _ := getModuleHandleW.Call(0)
	icon, _, _ := loadIconW.Call(0, idiApplication)
	cursor, _, _ := loadCursorW.Call(0, idcArrow)
	className := utf16Ptr(name)
	class := wndClassEx{
		Size: uint32(unsafe.Sizeof(wndClassEx{})), Style: csHRedraw | csVRedraw,
		WndProc: syscall.NewCallback(proc), Instance: instance,
		Icon: icon, Cursor: cursor, Background: colorWindow + 1, ClassName: className, IconSmall: icon,
	}
	atom, _, callErr := registerClassExW.Call(uintptr(unsafe.Pointer(&class)))
	if atom == 0 {
		return fmt.Errorf("注册 %s 失败: %w", name, callErr)
	}
	return nil
}

func createLabel(parent, instance uintptr, text string, x, y, width, height uintptr) uintptr {
	return createControl("STATIC", text, wsChild|wsVisible|ssLeft, x, y, width, height, parent, 0, instance)
}

func createControl(class, text string, style, x, y, width, height, parent, id, instance uintptr) uintptr {
	classPtr := utf16Ptr(class)
	textPtr := utf16Ptr(text)
	hwnd, _, _ := createWindowExW.Call(
		0, uintptr(unsafe.Pointer(classPtr)), uintptr(unsafe.Pointer(textPtr)), style,
		x, y, width, height, parent, id, instance, 0,
	)
	return hwnd
}

func readControlText(hwnd uintptr) string {
	length, _, _ := getWindowTextLengthW.Call(hwnd)
	if length == 0 {
		return ""
	}
	buffer := make([]uint16, int(length)+1)
	getWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	value := syscall.UTF16ToString(buffer)
	clear(buffer)
	return value
}

func setControlText(hwnd uintptr, value string) {
	ptr := utf16Ptr(value)
	setWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(ptr)))
}

func showMessage(owner uintptr, title, message string, flags uintptr) {
	titlePtr := utf16Ptr(title)
	messagePtr := utf16Ptr(message)
	messageBoxW.Call(owner, uintptr(unsafe.Pointer(messagePtr)), uintptr(unsafe.Pointer(titlePtr)), flags)
}
