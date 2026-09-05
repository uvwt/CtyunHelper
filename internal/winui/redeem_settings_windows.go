//go:build windows

package winui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/uvwt/CtyunHelper/internal/app"
	"github.com/uvwt/CtyunHelper/internal/automation"
)

const (
	bsAutoCheckbox  = 0x00000003
	cbsDropdownList = 0x00000003
	bmGetCheck      = 0x00F0
	bmSetCheck      = 0x00F1
	bstChecked      = 1
	cbAddString     = 0x0143
	cbGetCurSel     = 0x0147
	cbSetCurSel     = 0x014E

	redeemEnable       = 2301
	redeemDesktop      = 2302
	redeemProduct      = 2303
	redeemMaxTimes     = 2304
	redeemSchedule     = 2305
	redeemIntervalDays = 2306
	redeemMonthlyDays  = 2307
	redeemRefresh      = 2308
	redeemSave         = 2309
	redeemResolveOK    = 2310
	redeemResolveFail  = 2311

	wmRedeemCatalogReady = wmApp + 20
	wmRedeemSaved        = wmApp + 21
	wmRedeemResolved     = wmApp + 22
	mbYesNo              = 0x00000004
	mbIconWarning        = 0x00000030
	idYes                = 6
)

var (
	redeemClassOnce sync.Once
	redeemClassErr  error
	redeemDialogMu  sync.Mutex
	redeemDialog    *redeemDialogState
)

type redeemDialogState struct {
	mu sync.Mutex

	hwnd              uintptr
	owner             uintptr
	enabledCheck      uintptr
	desktopCombo      uintptr
	productCombo      uintptr
	maxTimesEdit      uintptr
	scheduleCombo     uintptr
	intervalEdit      uintptr
	monthlyEdit       uintptr
	statusText        uintptr
	refreshButton     uintptr
	saveButton        uintptr
	resolveOKButton   uintptr
	resolveFailButton uintptr

	settings  app.RedeemSettingsView
	catalog   app.RedeemCatalog
	resultErr error
	busy      bool
}

func openRedeemSettingsWindow(owner uintptr) {
	if uiRuntime == nil {
		return
	}
	redeemDialogMu.Lock()
	if redeemDialog != nil && redeemDialog.hwnd != 0 {
		hwnd := redeemDialog.hwnd
		redeemDialogMu.Unlock()
		showWindow.Call(hwnd, swShow)
		setForegroundWindow.Call(hwnd)
		return
	}
	redeemDialogMu.Unlock()

	if err := ensureRedeemWindowClass(); err != nil {
		showMessage(owner, "兑换设置", err.Error(), mbIconError)
		return
	}
	settings, err := uiRuntime.CurrentRedeemSettings()
	if err != nil {
		showMessage(owner, "兑换设置", err.Error(), mbIconError)
		return
	}

	instance, _, _ := getModuleHandleW.Call(0)
	className := utf16Ptr("CtyunHelperRedeemSettingsWindow")
	title := utf16Ptr("自动兑换设置")
	hwnd, _, callErr := createWindowExW.Call(
		0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)), wsOverlappedWindow,
		cwUseDefault, cwUseDefault, 680, 620,
		owner, 0, instance, 0,
	)
	if hwnd == 0 {
		showMessage(owner, "兑换设置", fmt.Sprintf("创建兑换设置窗口失败: %v", callErr), mbIconError)
		return
	}
	state := &redeemDialogState{hwnd: hwnd, owner: owner, settings: settings}
	createRedeemControls(state, instance)
	applyRedeemSettingsToControls(state)
	redeemDialogMu.Lock()
	redeemDialog = state
	redeemDialogMu.Unlock()
	if settings.Pending {
		setControlText(state.statusText, fmt.Sprintf("存在待确认兑换：%s，计划 %d 次 / %d 积分。请先人工核对服务端结果。", settings.PendingDate, settings.PendingTimes, settings.PendingPoints))
		showWindow.Call(state.resolveOKButton, swShow)
		showWindow.Call(state.resolveFailButton, swShow)
	} else {
		setControlText(state.statusText, "修改启用计划前请先刷新目录；关闭自动兑换可直接保存。")
		showWindow.Call(state.resolveOKButton, swHide)
		showWindow.Call(state.resolveFailButton, swHide)
	}
	showWindow.Call(hwnd, swShow)
	updateWindow.Call(hwnd)
	// 进入设置页就刷新目录，保留手动刷新按钮作为网络失败后的重试入口。
	startRedeemCatalogLoad(state)
}

func createRedeemControls(state *redeemDialogState, instance uintptr) {
	state.enabledCheck = createControl("BUTTON", "启用自动兑换", wsChild|wsVisible|wsTabStop|bsAutoCheckbox, 28, 24, 150, 28, state.hwnd, redeemEnable, instance)

	createLabel(state.hwnd, instance, "绑定云电脑", 28, 76, 100, 24)
	state.desktopCombo = createControl("COMBOBOX", "", wsChild|wsVisible|wsTabStop|cbsDropdownList, 140, 72, 470, 300, state.hwnd, redeemDesktop, instance)

	createLabel(state.hwnd, instance, "兑换商品", 28, 122, 100, 24)
	state.productCombo = createControl("COMBOBOX", "", wsChild|wsVisible|wsTabStop|cbsDropdownList, 140, 118, 470, 300, state.hwnd, redeemProduct, instance)

	createLabel(state.hwnd, instance, "最大次数", 28, 168, 100, 24)
	state.maxTimesEdit = createControl("EDIT", "", wsChild|wsVisible|wsBorder|wsTabStop|esAutoHScroll, 140, 164, 110, 28, state.hwnd, redeemMaxTimes, instance)
	createLabel(state.hwnd, instance, "0 = 按当前积分尽量兑换", 270, 168, 250, 24)

	createLabel(state.hwnd, instance, "执行策略", 28, 214, 100, 24)
	state.scheduleCombo = createControl("COMBOBOX", "", wsChild|wsVisible|wsTabStop|cbsDropdownList, 140, 210, 210, 200, state.hwnd, redeemSchedule, instance)
	setComboStrings(state.scheduleCombo, []string{"每天", "每隔 N 天", "每月指定日期"}, 0)

	createLabel(state.hwnd, instance, "间隔天数", 28, 260, 100, 24)
	state.intervalEdit = createControl("EDIT", "", wsChild|wsVisible|wsBorder|wsTabStop|esAutoHScroll, 140, 256, 110, 28, state.hwnd, redeemIntervalDays, instance)
	createLabel(state.hwnd, instance, "仅“每隔 N 天”使用", 270, 260, 220, 24)

	createLabel(state.hwnd, instance, "每月日期", 28, 306, 100, 24)
	state.monthlyEdit = createControl("EDIT", "", wsChild|wsVisible|wsBorder|wsTabStop|esAutoHScroll, 140, 302, 300, 28, state.hwnd, redeemMonthlyDays, instance)
	createLabel(state.hwnd, instance, "逗号分隔；-1 表示月末，例如 1,15,-1", 28, 338, 500, 24)

	state.statusText = createLabel(state.hwnd, instance, "", 28, 374, 590, 48)
	state.resolveOKButton = createControl("BUTTON", "已确认兑换成功", wsChild|wsVisible|wsTabStop|bsPushButton, 90, 430, 180, 34, state.hwnd, redeemResolveOK, instance)
	state.resolveFailButton = createControl("BUTTON", "已确认未兑换", wsChild|wsVisible|wsTabStop|bsPushButton, 290, 430, 180, 34, state.hwnd, redeemResolveFail, instance)
	state.refreshButton = createControl("BUTTON", "刷新云电脑 / 商品", wsChild|wsVisible|wsTabStop|bsPushButton, 140, 486, 160, 36, state.hwnd, redeemRefresh, instance)
	state.saveButton = createControl("BUTTON", "保存并立即生效", wsChild|wsVisible|wsTabStop|bsPushButton, 320, 486, 160, 36, state.hwnd, redeemSave, instance)
}

func applyRedeemSettingsToControls(state *redeemDialogState) {
	if state.settings.Enabled {
		sendMessageW.Call(state.enabledCheck, bmSetCheck, bstChecked, 0)
	}
	setControlText(state.maxTimesEdit, strconv.Itoa(state.settings.MaxRedeemTimes))
	interval := state.settings.IntervalDays
	if interval <= 0 {
		interval = 1
	}
	setControlText(state.intervalEdit, strconv.Itoa(interval))
	parts := make([]string, 0, len(state.settings.MonthlyDays))
	for _, day := range state.settings.MonthlyDays {
		parts = append(parts, strconv.Itoa(day))
	}
	setControlText(state.monthlyEdit, strings.Join(parts, ","))

	scheduleIndex := 0
	switch state.settings.ScheduleType {
	case automation.RedeemScheduleInterval:
		scheduleIndex = 1
	case automation.RedeemScheduleMonthlyDays:
		scheduleIndex = 2
	}
	sendMessageW.Call(state.scheduleCombo, cbSetCurSel, uintptr(scheduleIndex), 0)
}

func redeemSettingsWindowProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	redeemDialogMu.Lock()
	state := redeemDialog
	redeemDialogMu.Unlock()
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
		case redeemRefresh:
			startRedeemCatalogLoad(state)
		case redeemSave:
			startRedeemSettingsSave(state)
		case redeemResolveOK:
			startRedeemPendingResolve(state, true)
		case redeemResolveFail:
			startRedeemPendingResolve(state, false)
		}
		return 0
	case wmRedeemCatalogReady:
		state.mu.Lock()
		err := state.resultErr
		state.resultErr = nil
		state.busy = false
		catalog := state.catalog
		state.mu.Unlock()
		setRedeemDialogBusy(state, false)
		if err != nil {
			setControlText(state.statusText, "目录加载失败："+err.Error()+"。关闭自动兑换仍可保存。")
			return 0
		}
		populateRedeemCatalog(state, catalog)
		if state.settings.Pending {
			setControlText(state.statusText, fmt.Sprintf("已加载 %d 台云电脑、%d 个可兑换商品；仍有一笔兑换待人工确认。", len(catalog.Desktops), len(catalog.Products)))
		} else {
			setControlText(state.statusText, fmt.Sprintf("已加载 %d 台云电脑、%d 个可兑换商品。", len(catalog.Desktops), len(catalog.Products)))
		}
		return 0
	case wmRedeemSaved:
		state.mu.Lock()
		err := state.resultErr
		state.resultErr = nil
		state.busy = false
		state.mu.Unlock()
		setRedeemDialogBusy(state, false)
		if err != nil {
			showMessage(hwnd, "保存兑换设置", err.Error(), mbIconError)
			return 0
		}
		showMessage(hwnd, "兑换设置", "已保存并立即生效。", mbInformation)
		destroyWindow.Call(hwnd)
		return 0
	case wmRedeemResolved:
		state.mu.Lock()
		err := state.resultErr
		state.resultErr = nil
		state.busy = false
		state.mu.Unlock()
		setRedeemDialogBusy(state, false)
		if err != nil {
			showMessage(hwnd, "确认兑换结果", err.Error(), mbIconError)
			return 0
		}
		showMessage(hwnd, "确认兑换结果", "不确定状态已按你的人工核对结果处理。", mbInformation)
		destroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		redeemDialogMu.Lock()
		if redeemDialog == state {
			redeemDialog = nil
		}
		redeemDialogMu.Unlock()
		return 0
	}
	result, _, _ := defWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return result
}

func startRedeemCatalogLoad(state *redeemDialogState) {
	if !beginDialogWork(&state.mu, &state.busy) {
		return
	}
	setRedeemDialogBusy(state, true)
	if state.settings.Pending {
		setControlText(state.statusText, "正在加载云电脑和商品目录…；当前仍有一笔兑换待人工确认。")
	} else {
		setControlText(state.statusText, "正在加载云电脑和商品目录…")
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		catalog, err := uiRuntime.LoadRedeemCatalog(ctx)
		state.mu.Lock()
		state.catalog = catalog
		state.resultErr = err
		state.mu.Unlock()
		postMessageW.Call(state.hwnd, wmRedeemCatalogReady, 0, 0)
	}()
}

func startRedeemSettingsSave(state *redeemDialogState) {
	if !beginDialogWork(&state.mu, &state.busy) {
		return
	}
	request, err := readRedeemSettingsRequest(state)
	if err != nil {
		endDialogWork(&state.mu, &state.busy)
		showMessage(state.hwnd, "兑换设置", err.Error(), mbIconError)
		return
	}
	setRedeemDialogBusy(state, true)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		err := uiRuntime.SaveRedeemSettings(ctx, request)
		state.mu.Lock()
		state.resultErr = err
		state.mu.Unlock()
		postMessageW.Call(state.hwnd, wmRedeemSaved, 0, 0)
	}()
}

func startRedeemPendingResolve(state *redeemDialogState, succeeded bool) {
	state.mu.Lock()
	settings := state.settings
	state.mu.Unlock()
	if !settings.Pending {
		showMessage(state.hwnd, "确认兑换结果", "当前没有待人工确认的兑换。", mbIconError)
		return
	}
	resultText := "未兑换"
	if succeeded {
		resultText = "兑换成功"
	}
	message := fmt.Sprintf(
		"请仅在你已经人工核对天翼兑换记录后继续。\n\n待确认日期：%s\n计划：%d 次 / %d 积分\n你确认结果为：%s\n\n确认后当天不会再次自动下单。",
		settings.PendingDate, settings.PendingTimes, settings.PendingPoints, resultText,
	)
	if !confirmRedeemResult(state.hwnd, message) {
		return
	}
	if !beginDialogWork(&state.mu, &state.busy) {
		return
	}
	setRedeemDialogBusy(state, true)
	go func() {
		err := uiRuntime.ResolvePendingRedeem(succeeded)
		state.mu.Lock()
		state.resultErr = err
		state.mu.Unlock()
		postMessageW.Call(state.hwnd, wmRedeemResolved, 0, 0)
	}()
}

func confirmRedeemResult(owner uintptr, message string) bool {
	titlePtr := utf16Ptr("确认兑换结果")
	messagePtr := utf16Ptr(message)
	result, _, _ := messageBoxW.Call(
		owner,
		uintptr(unsafe.Pointer(messagePtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		mbYesNo|mbIconWarning,
	)
	return result == idYes
}

func readRedeemSettingsRequest(state *redeemDialogState) (app.SaveRedeemSettingsRequest, error) {
	checked, _, _ := sendMessageW.Call(state.enabledCheck, bmGetCheck, 0, 0)
	request := app.SaveRedeemSettingsRequest{Enabled: checked == bstChecked}
	if !request.Enabled {
		return request, nil
	}

	state.mu.Lock()
	catalog := state.catalog
	state.mu.Unlock()
	if len(catalog.Desktops) == 0 || len(catalog.Products) == 0 {
		return request, fmt.Errorf("请先刷新云电脑 / 商品目录")
	}
	desktopIndex := comboSelection(state.desktopCombo)
	productIndex := comboSelection(state.productCombo)
	if desktopIndex < 0 || desktopIndex >= len(catalog.Desktops) {
		return request, fmt.Errorf("请先选择绑定云电脑")
	}
	if productIndex < 0 || productIndex >= len(catalog.Products) {
		return request, fmt.Errorf("请先选择兑换商品")
	}
	request.DesktopID = catalog.Desktops[desktopIndex].ID
	request.ProductID = catalog.Products[productIndex].ID
	request.ProductType = catalog.Products[productIndex].Type

	maxTimes, err := parseNonNegativeInt(readControlText(state.maxTimesEdit), "最大次数")
	if err != nil {
		return request, err
	}
	request.MaxRedeemTimes = maxTimes

	switch comboSelection(state.scheduleCombo) {
	case 0:
		request.ScheduleType = automation.RedeemScheduleDaily
		request.IntervalDays = 1
	case 1:
		request.ScheduleType = automation.RedeemScheduleInterval
		interval, err := parsePositiveInt(readControlText(state.intervalEdit), "间隔天数")
		if err != nil {
			return request, err
		}
		request.IntervalDays = interval
	case 2:
		request.ScheduleType = automation.RedeemScheduleMonthlyDays
		days, err := parseMonthlyDays(readControlText(state.monthlyEdit))
		if err != nil {
			return request, err
		}
		request.MonthlyDays = days
		request.IntervalDays = 1
	default:
		return request, fmt.Errorf("请选择执行策略")
	}
	return request, nil
}

func populateRedeemCatalog(state *redeemDialogState, catalog app.RedeemCatalog) {
	desktopLabels := make([]string, 0, len(catalog.Desktops))
	desktopSelected := -1
	for index, desktop := range catalog.Desktops {
		desktopLabels = append(desktopLabels, desktop.Name)
		if desktop.ID == state.settings.DesktopID {
			desktopSelected = index
		}
	}
	setComboStrings(state.desktopCombo, desktopLabels, desktopSelected)

	productLabels := make([]string, 0, len(catalog.Products))
	productSelected := -1
	for index, product := range catalog.Products {
		productLabels = append(productLabels, fmt.Sprintf("%s（%d 积分）", product.Name, product.CostPoints))
		if product.ID == state.settings.ProductID && product.Type == state.settings.ProductType {
			productSelected = index
		}
	}
	setComboStrings(state.productCombo, productLabels, productSelected)
}

func setComboStrings(control uintptr, values []string, selected int) {
	// CB_RESETCONTENT = 0x014B
	sendMessageW.Call(control, 0x014B, 0, 0)
	for _, value := range values {
		ptr := utf16Ptr(value)
		sendMessageW.Call(control, cbAddString, 0, uintptr(unsafe.Pointer(ptr)))
	}
	if selected >= 0 && selected < len(values) {
		sendMessageW.Call(control, cbSetCurSel, uintptr(selected), 0)
	} else {
		// 旧配置找不到对应项时必须保持“未选择”，不能自动落到第一项。
		// 兑换目标属于会消费积分的高价值选择，要求用户显式确认新目标。
		sendMessageW.Call(control, cbSetCurSel, ^uintptr(0), 0)
	}
}

func comboSelection(control uintptr) int {
	value, _, _ := sendMessageW.Call(control, cbGetCurSel, 0, 0)
	if int32(value) == -1 {
		return -1
	}
	return int(value)
}

func parseNonNegativeInt(value, label string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("%s必须是大于等于 0 的整数", label)
	}
	return parsed, nil
}

func parsePositiveInt(value, label string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s必须是大于 0 的整数", label)
	}
	return parsed, nil
}

func parseMonthlyDays(value string) ([]int, error) {
	parts := strings.Split(value, ",")
	result := make([]int, 0, len(parts))
	seen := make(map[int]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		day, err := strconv.Atoi(part)
		if err != nil || (day != -1 && (day < 1 || day > 31)) {
			return nil, fmt.Errorf("每月日期只能填写 1-31 或 -1（月末）")
		}
		if _, exists := seen[day]; exists {
			continue
		}
		seen[day] = struct{}{}
		result = append(result, day)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("请至少填写一个每月执行日期")
	}
	return result, nil
}

func setRedeemDialogBusy(state *redeemDialogState, busy bool) {
	enabled := uintptr(1)
	if busy {
		enabled = 0
	}
	enableWindow.Call(state.refreshButton, enabled)
	enableWindow.Call(state.saveButton, enabled)
	enableWindow.Call(state.resolveOKButton, enabled)
	enableWindow.Call(state.resolveFailButton, enabled)
}

func ensureRedeemWindowClass() error {
	redeemClassOnce.Do(func() {
		redeemClassErr = registerDialogWindowClass("CtyunHelperRedeemSettingsWindow", redeemSettingsWindowProc)
	})
	return redeemClassErr
}
