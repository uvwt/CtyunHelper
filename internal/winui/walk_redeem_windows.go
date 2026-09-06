//go:build windows

package winui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tailscale/walk"
	d "github.com/tailscale/walk/declarative"
	"github.com/uvwt/CtyunHelper/internal/app"
	"github.com/uvwt/CtyunHelper/internal/automation"
)

func (v *walkMainView) openRedeemSettings() {
	settings, err := v.runtime.CurrentRedeemSettings()
	if err != nil {
		walk.MsgBox(v.window, "兑换设置", err.Error(), walk.MsgBoxIconError|walk.MsgBoxOK)
		return
	}

	var (
		dlg           *walk.Dialog
		enabledCheck  *walk.CheckBox
		desktopCombo  *walk.ComboBox
		productCombo  *walk.ComboBox
		maxTimesEdit  *walk.NumberEdit
		scheduleCombo *walk.ComboBox
		intervalEdit  *walk.NumberEdit
		monthlyEdit   *walk.LineEdit
		statusLabel   *walk.Label
		pendingGroup  *walk.GroupBox
		refreshButton *walk.PushButton
		saveButton    *walk.PushButton
		cancelButton  *walk.PushButton
		catalog       app.RedeemCatalog
		catalogLoaded bool
		busy          bool
	)

	setBusy := func(value bool) {
		busy = value
		if refreshButton != nil {
			refreshButton.SetEnabled(!value)
		}
		if saveButton != nil {
			saveButton.SetEnabled(!value)
		}
	}

	selectConfigured := func() {
		desktopIndex := -1
		for index, item := range catalog.Desktops {
			if item.ID == settings.DesktopID {
				desktopIndex = index
				break
			}
		}
		productIndex := -1
		for index, item := range catalog.Products {
			if item.ID == settings.ProductID && item.Type == settings.ProductType {
				productIndex = index
				break
			}
		}
		_ = desktopCombo.SetCurrentIndex(desktopIndex)
		_ = productCombo.SetCurrentIndex(productIndex)
	}

	var loadCatalog func()
	loadCatalog = func() {
		if busy {
			return
		}
		setBusy(true)
		_ = statusLabel.SetText("正在加载可绑定云电脑和兑换商品…")
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			newCatalog, err := v.runtime.LoadRedeemCatalog(ctx)
			walk.App().Synchronize(func() {
				setBusy(false)
				if err != nil {
					catalogLoaded = false
					_ = statusLabel.SetText("目录加载失败；仍可关闭自动兑换。")
					walk.MsgBox(dlg, "兑换设置", err.Error(), walk.MsgBoxIconError|walk.MsgBoxOK)
					return
				}
				catalog = newCatalog
				catalogLoaded = true
				desktops := make([]string, 0, len(catalog.Desktops))
				for _, item := range catalog.Desktops {
					desktops = append(desktops, item.Name)
				}
				products := make([]string, 0, len(catalog.Products))
				for _, item := range catalog.Products {
					products = append(products, fmt.Sprintf("%s（%d 积分）", item.Name, item.CostPoints))
				}
				_ = desktopCombo.SetModel(desktops)
				_ = productCombo.SetModel(products)
				selectConfigured()
				_ = statusLabel.SetText(fmt.Sprintf("已加载 %d 台云电脑、%d 个兑换商品。", len(catalog.Desktops), len(catalog.Products)))
			})
		}()
	}

	save := func() {
		if busy {
			return
		}
		request := app.SaveRedeemSettingsRequest{Enabled: enabledCheck.Checked()}
		if request.Enabled {
			if !catalogLoaded {
				walk.MsgBox(dlg, "兑换设置", "请先成功刷新兑换目录。", walk.MsgBoxIconWarning|walk.MsgBoxOK)
				return
			}
			desktopIndex := desktopCombo.CurrentIndex()
			productIndex := productCombo.CurrentIndex()
			if desktopIndex < 0 || desktopIndex >= len(catalog.Desktops) || productIndex < 0 || productIndex >= len(catalog.Products) {
				walk.MsgBox(dlg, "兑换设置", "请选择云电脑和兑换商品。", walk.MsgBoxIconWarning|walk.MsgBoxOK)
				return
			}
			request.DesktopID = catalog.Desktops[desktopIndex].ID
			request.ProductID = catalog.Products[productIndex].ID
			request.ProductType = catalog.Products[productIndex].Type
			request.MaxRedeemTimes = int(maxTimesEdit.Value())
			request.IntervalDays = int(intervalEdit.Value())
			if request.IntervalDays <= 0 {
				request.IntervalDays = 1
			}
			switch scheduleCombo.CurrentIndex() {
			case 1:
				request.ScheduleType = automation.RedeemScheduleInterval
			case 2:
				request.ScheduleType = automation.RedeemScheduleMonthlyDays
				days, err := parseWalkMonthlyDays(monthlyEdit.Text())
				if err != nil {
					walk.MsgBox(dlg, "兑换设置", err.Error(), walk.MsgBoxIconWarning|walk.MsgBoxOK)
					return
				}
				request.MonthlyDays = days
			default:
				request.ScheduleType = automation.RedeemScheduleDaily
			}
		}

		setBusy(true)
		_ = statusLabel.SetText("正在保存兑换设置…")
		go func(request app.SaveRedeemSettingsRequest) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			err := v.runtime.SaveRedeemSettings(ctx, request)
			walk.App().Synchronize(func() {
				setBusy(false)
				if err != nil {
					_ = statusLabel.SetText("保存失败。")
					walk.MsgBox(dlg, "兑换设置", err.Error(), walk.MsgBoxIconError|walk.MsgBoxOK)
					return
				}
				dlg.Accept()
			})
		}(request)
	}

	resolvePending := func(succeeded bool) {
		text := "确认上一笔兑换没有成功吗？"
		if succeeded {
			text = "确认上一笔兑换已经成功吗？"
		}
		if walk.MsgBox(dlg, "确认兑换结果", text, walk.MsgBoxIconQuestion|walk.MsgBoxYesNo) != walk.DlgCmdYes {
			return
		}
		if err := v.runtime.ResolvePendingRedeem(succeeded); err != nil {
			walk.MsgBox(dlg, "确认兑换结果", err.Error(), walk.MsgBoxIconError|walk.MsgBoxOK)
			return
		}
		if pendingGroup != nil {
			pendingGroup.SetVisible(false)
		}
		settings.Pending = false
		_ = statusLabel.SetText("上一笔兑换结果已确认。")
	}

	dialogChildren := []d.Widget{
		d.CheckBox{AssignTo: &enabledCheck, Text: "启用自动兑换"},
		d.GroupBox{Title: "兑换目标", Layout: d.Grid{Columns: 2, Spacing: 8}, Children: []d.Widget{
			d.Label{Text: "云电脑", MinSize: d.Size{Width: 110}},
			d.ComboBox{AssignTo: &desktopCombo, Model: []string{"正在加载…"}, StretchFactor: 1},
			d.Label{Text: "兑换商品", MinSize: d.Size{Width: 110}},
			d.ComboBox{AssignTo: &productCombo, Model: []string{"正在加载…"}, StretchFactor: 1},
			d.Label{Text: "单次最多兑换", MinSize: d.Size{Width: 110}},
			d.NumberEdit{AssignTo: &maxTimesEdit, MinValue: 0, MaxValue: 100, SpinButtonsVisible: true},
		}},
		d.GroupBox{Title: "执行策略", Layout: d.Grid{Columns: 2, Spacing: 8}, Children: []d.Widget{
			d.Label{Text: "策略", MinSize: d.Size{Width: 110}},
			d.ComboBox{AssignTo: &scheduleCombo, Model: []string{"每天", "每隔 N 天", "每月指定日期"}, StretchFactor: 1},
			d.Label{Text: "间隔天数", MinSize: d.Size{Width: 110}},
			d.NumberEdit{AssignTo: &intervalEdit, MinValue: 1, MaxValue: 365, SpinButtonsVisible: true},
			d.Label{Text: "每月日期", MinSize: d.Size{Width: 110}},
			d.LineEdit{AssignTo: &monthlyEdit, CueBanner: "例如 1,15,-1（-1 表示月末）", StretchFactor: 1},
		}},
	}
	// Walk 会在 Dialog.Run 时重新显示已创建的普通子控件，所以非 pending
	// 状态下不创建这组控件，确保它既不可见，也完全不参与布局。
	if settings.Pending {
		dialogChildren = append(dialogChildren, d.GroupBox{
			AssignTo: &pendingGroup,
			Title:    "兑换结果待确认",
			Layout:   d.VBox{Spacing: 6},
			Children: []d.Widget{
				d.Label{Text: fmt.Sprintf("%s：程序无法确定上一笔兑换是否成功（%d 次，预计 %d 积分），请核对天翼云后确认结果。", settings.PendingDate, settings.PendingTimes, settings.PendingPoints), EllipsisMode: d.EllipsisNone},
				d.Composite{Layout: d.HBox{MarginsZero: true, Spacing: 8}, Children: []d.Widget{
					d.PushButton{Text: "确认已成功", OnClicked: func() { resolvePending(true) }},
					d.PushButton{Text: "确认未成功", OnClicked: func() { resolvePending(false) }},
					d.HSpacer{},
				}},
			},
		})
	}
	dialogChildren = append(dialogChildren,
		d.Label{AssignTo: &statusLabel, Text: "正在加载兑换目录…", EllipsisMode: d.EllipsisNone},
		d.HSpacer{},
		d.Composite{Layout: d.HBox{MarginsZero: true, Spacing: 8}, Children: []d.Widget{
			d.PushButton{AssignTo: &refreshButton, Text: "刷新目录", OnClicked: func() { loadCatalog() }},
			d.HSpacer{},
			d.PushButton{AssignTo: &saveButton, Text: "保存", OnClicked: save},
			d.PushButton{AssignTo: &cancelButton, Text: "取消", OnClicked: func() { dlg.Cancel() }},
		}},
	)

	if err := (d.Dialog{
		AssignTo:      &dlg,
		Title:         "兑换设置",
		Size:          d.Size{Width: 590, Height: 590},
		MinSize:       d.Size{Width: 520, Height: 470},
		Layout:        d.VBox{Margins: d.Margins{Left: 12, Top: 12, Right: 12, Bottom: 12}, Spacing: 8},
		Children:      dialogChildren,
		DefaultButton: &saveButton,
		CancelButton:  &cancelButton,
	}).Create(v.window); err != nil {
		walk.MsgBox(v.window, "兑换设置", err.Error(), walk.MsgBoxIconError|walk.MsgBoxOK)
		return
	}
	defer dlg.Dispose()
	if icon, err := walk.NewIconFromResourceId(1); err == nil {
		_ = dlg.SetIcon(icon)
	}

	enabledCheck.SetChecked(settings.Enabled)
	_ = maxTimesEdit.SetValue(float64(settings.MaxRedeemTimes))
	interval := settings.IntervalDays
	if interval <= 0 {
		interval = 1
	}
	_ = intervalEdit.SetValue(float64(interval))
	_ = monthlyEdit.SetText(formatWalkMonthlyDays(settings.MonthlyDays))
	scheduleIndex := 0
	switch settings.ScheduleType {
	case automation.RedeemScheduleInterval:
		scheduleIndex = 1
	case automation.RedeemScheduleMonthlyDays:
		scheduleIndex = 2
	}
	_ = scheduleCombo.SetCurrentIndex(scheduleIndex)
	loadCatalog()
	dlg.Run()
}

func parseWalkMonthlyDays(text string) ([]int, error) {
	parts := strings.FieldsFunc(strings.TrimSpace(text), func(r rune) bool {
		return r == ',' || r == '，' || r == ';' || r == '；' || r == ' '
	})
	if len(parts) == 0 {
		return nil, fmt.Errorf("每月指定日期不能为空")
	}
	days := make([]int, 0, len(parts))
	seen := make(map[int]bool, len(parts))
	for _, part := range parts {
		day, err := strconv.Atoi(part)
		if err != nil || (day != -1 && (day < 1 || day > 31)) {
			return nil, fmt.Errorf("无效的每月日期 %q；请输入 1-31 或 -1（月末）", part)
		}
		if !seen[day] {
			seen[day] = true
			days = append(days, day)
		}
	}
	return days, nil
}

func formatWalkMonthlyDays(days []int) string {
	parts := make([]string, 0, len(days))
	for _, day := range days {
		parts = append(parts, strconv.Itoa(day))
	}
	return strings.Join(parts, ",")
}
