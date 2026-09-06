//go:build windows

package winui

import (
	"context"
	"fmt"
	"sync"

	"github.com/tailscale/walk"
	d "github.com/tailscale/walk/declarative"
	"github.com/uvwt/CtyunHelper/internal/app"
	"golang.org/x/sys/windows"
)

const walkWindowTitle = "天翼云电脑助手"

type walkMainView struct {
	runtime *app.Runtime
	model   *app.Model
	window  *walk.MainWindow
	tray    *walk.NotifyIcon

	keepalive     *walk.TextLabel
	desktop       *walk.TextLabel
	points        *walk.TextLabel
	pointsSync    *walk.TextLabel
	redeem        *walk.TextLabel
	redeemDesktop *walk.TextLabel
	redeemProduct *walk.TextLabel
	lastError     *walk.TextLabel
	loginAITask   *walk.TextLabel
	usageTask     *walk.TextLabel
	aiPointsTask  *walk.TextLabel

	loginButton          *walk.PushButton
	bindButton           *walk.PushButton
	aiButton             *walk.PushButton
	pointsButton         *walk.PushButton
	redeemButton         *walk.PushButton
	redeemSettingsButton *walk.PushButton
	settingsButton       *walk.PushButton
	logsButton           *walk.PushButton
	logoutButton         *walk.PushButton
	aboutButton          *walk.PushButton

	mu       sync.Mutex
	quitting bool
}

func Run(buildRuntime func() (*app.Runtime, error), options RunOptions) error {
	if buildRuntime == nil {
		return fmt.Errorf("Windows UI 缺少 Runtime 构造函数")
	}

	// Per-Monitor V2 DPI awareness 由构建时嵌入的 manifest 声明，避免运行期
	// 再直接操作 DPI Win32 API；Walk 负责后续控件和布局的 DPI 变化。
	walkApp, err := walk.InitApp()
	if err != nil {
		return fmt.Errorf("初始化 Windows UI: %w", err)
	}
	walkApp.SetOrganizationName("uvwt")
	walkApp.SetProductName("CtyunHelper")

	mutex, alreadyRunning, err := acquireSingleInstance()
	if err != nil {
		return err
	}
	defer windows.CloseHandle(mutex)
	if alreadyRunning {
		activateExistingWalkWindow()
		return nil
	}

	appRuntime, err := buildRuntime()
	if err != nil {
		return err
	}
	view := &walkMainView{runtime: appRuntime, model: appRuntime.Model()}
	if err := view.create(); err != nil {
		return err
	}
	defer view.dispose()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	appRuntime.Start(ctx)
	defer appRuntime.Stop()

	events, unsubscribe := view.model.Events().Subscribe(64)
	defer unsubscribe()
	go view.observe(ctx, events)

	view.applyState(view.model.Snapshot())
	if !options.StartHidden {
		view.window.Show()
	}

	walkApp.Run()
	return nil
}

func (v *walkMainView) create() error {
	caption := func(text string) d.Label {
		return d.Label{Text: text, MinSize: d.Size{Width: 105}, MaxSize: d.Size{Width: 135}}
	}
	value := func(target **walk.TextLabel) d.TextLabel {
		// TextLabel 本身具备 GrowableHorz，并能随可用宽度换行；普通 Label
		// 只按文字理想宽度参与布局，会让 ScrollView 内的状态卡片被居中挤到右侧。
		return d.TextLabel{
			AssignTo:      target,
			MinSize:       d.Size{Width: 160},
			StretchFactor: 1,
			TextAlignment: d.AlignHNearVCenter,
		}
	}

	if err := (d.MainWindow{
		AssignTo: &v.window,
		Title:    walkWindowTitle,
		Size:     d.Size{Width: 720, Height: 480},
		MinSize:  d.Size{Width: 620, Height: 400},
		Visible:  false,
		Layout:   d.VBox{Margins: d.Margins{Left: 12, Top: 12, Right: 12, Bottom: 12}, Spacing: 8, Alignment: d.AlignHNearVNear},
		Children: []d.Widget{
			d.ScrollView{
				// Walk 只有启用水平滚动能力时才给 ScrollView GrowableHorz；
				// 实际内容宽度由内部布局跟随客户区，因此正常不会出现水平滚动条。
				HorizontalFixed: false,
				Layout:          d.VBox{MarginsZero: true, Spacing: 8, Alignment: d.AlignHNearVNear},
				Children: []d.Widget{
					d.GroupBox{
						Title:  "核心状态",
						Layout: d.Grid{Columns: 2, Spacing: 8},
						Children: []d.Widget{
							caption("保活状态"), value(&v.keepalive),
							caption("当前云电脑"), value(&v.desktop),
							caption("当前积分"), value(&v.points),
							caption("积分同步"), value(&v.pointsSync),
							caption("最近错误"), value(&v.lastError),
						},
					},
					d.GroupBox{
						Title:  "自动兑换",
						Layout: d.Grid{Columns: 2, Spacing: 8},
						Children: []d.Widget{
							caption("兑换状态"), value(&v.redeem),
							caption("目标云电脑"), value(&v.redeemDesktop),
							caption("目标商品"), value(&v.redeemProduct),
						},
					},
					d.GroupBox{
						Title:  "积分任务",
						Layout: d.Grid{Columns: 2, Spacing: 8},
						Children: []d.Widget{
							caption("登录 AI"), value(&v.loginAITask),
							caption("使用 1 小时"), value(&v.usageTask),
							caption("AI 积分"), value(&v.aiPointsTask),
						},
					},
					d.GroupBox{
						Title:  "快捷操作",
						Layout: d.Grid{Columns: 2, Spacing: 8},
						Children: []d.Widget{
							d.PushButton{AssignTo: &v.loginButton, Text: "账号登录", OnClicked: v.openLogin},
							d.PushButton{AssignTo: &v.bindButton, Text: "绑定设备", OnClicked: v.openBinding},
							d.PushButton{AssignTo: &v.aiButton, Text: "执行 AI 任务", OnClicked: func() { v.runTask("AI 任务", v.runtime.RunAITask) }},
							d.PushButton{AssignTo: &v.pointsButton, Text: "刷新积分", OnClicked: func() { v.runTask("刷新积分", v.runtime.RunPointsTask) }},
							d.PushButton{AssignTo: &v.redeemButton, Text: "检查兑换", OnClicked: func() { v.runTask("检查兑换", v.runtime.RunRedeemTask) }},
							d.PushButton{AssignTo: &v.redeemSettingsButton, Text: "兑换设置", OnClicked: v.openRedeemSettings},
							d.PushButton{AssignTo: &v.settingsButton, Text: "设置", OnClicked: v.openSettings},
							d.PushButton{AssignTo: &v.logsButton, Text: "日志", OnClicked: v.openLogs},
							d.PushButton{AssignTo: &v.logoutButton, Text: "退出账号", OnClicked: v.logout},
							d.PushButton{AssignTo: &v.aboutButton, Text: "关于", OnClicked: v.openAbout},
						},
					},
				},
			},
		},
	}).Create(); err != nil {
		return fmt.Errorf("创建主窗口: %w", err)
	}

	if icon, err := walk.NewIconFromResourceId(1); err == nil {
		_ = v.window.SetIcon(icon)
	}

	// MainWindow 默认会在收到 WM_CLOSE 后调用 Application.Exit，即便 Closing
	// handler 已取消关闭。托盘应用必须关闭这个默认行为，真正退出只走托盘“退出”。
	v.window.SetExitOnClose(false)
	v.window.Closing().Attach(func(canceled *bool, _ walk.CloseReason) {
		v.mu.Lock()
		quitting := v.quitting
		v.mu.Unlock()
		if !quitting {
			*canceled = true
			v.window.Hide()
		}
	})

	return v.createTray()
}

func (v *walkMainView) createTray() error {
	tray, err := walk.NewNotifyIcon()
	if err != nil {
		return fmt.Errorf("创建系统托盘: %w", err)
	}
	v.tray = tray
	if icon, err := walk.NewIconFromResourceId(1); err == nil {
		_ = tray.SetIcon(icon)
	} else {
		_ = tray.SetIcon(walk.IconApplication())
	}
	_ = tray.SetToolTip(walkWindowTitle)

	tray.MouseDown().Attach(func(_, _ int, button walk.MouseButton) {
		if button == walk.LeftButton {
			v.showMainWindow()
		}
	})

	add := func(text string, fn func()) error {
		action := walk.NewAction()
		if err := action.SetText(text); err != nil {
			return err
		}
		action.Triggered().Attach(fn)
		return tray.ContextMenu().Actions().Add(action)
	}
	if err := add("打开主界面", v.showMainWindow); err != nil {
		return err
	}
	if err := add("执行 AI 任务", func() { v.runTask("AI 任务", v.runtime.RunAITask) }); err != nil {
		return err
	}
	if err := add("刷新积分", func() { v.runTask("刷新积分", v.runtime.RunPointsTask) }); err != nil {
		return err
	}
	if err := add("检查兑换", func() { v.runTask("检查兑换", v.runtime.RunRedeemTask) }); err != nil {
		return err
	}
	if err := add("兑换设置", v.openRedeemSettings); err != nil {
		return err
	}
	if err := add("设置", v.openSettings); err != nil {
		return err
	}
	if err := add("日志", v.openLogs); err != nil {
		return err
	}
	if err := add("关于", v.openAbout); err != nil {
		return err
	}
	if err := add("退出账号", v.logout); err != nil {
		return err
	}
	if err := tray.ContextMenu().Actions().Add(walk.NewSeparatorAction()); err != nil {
		return err
	}
	if err := add("退出", v.quit); err != nil {
		return err
	}

	if err := tray.SetVisible(true); err != nil {
		return fmt.Errorf("显示系统托盘: %w", err)
	}
	return nil
}

func (v *walkMainView) dispose() {
	if v.tray != nil {
		v.tray.Dispose()
	}
	closeWalkDialogs()
	if v.window != nil {
		v.window.Dispose()
	}
}

func (v *walkMainView) observe(ctx context.Context, events <-chan app.Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if event.Type != app.EventStateChanged {
				continue
			}
			state, ok := event.Data.(app.State)
			if !ok {
				continue
			}
			walk.App().Synchronize(func() { v.applyState(state) })
		}
	}
}

func (v *walkMainView) applyState(state app.State) {
	jobsRunning := state.AITask.Running || state.PointsTask.Running || state.RedeemTask.Running
	if state.Account == "" {
		_ = v.loginButton.SetText("账号登录")
	} else {
		_ = v.loginButton.SetText("更换账号")
	}
	v.loginButton.SetEnabled(!jobsRunning)
	v.bindButton.SetEnabled(state.Connection == app.ConnectionDeviceBind)
	v.aiButton.SetEnabled(state.Account != "" && !state.AutomationPaused && state.Connection != app.ConnectionAuth && state.Connection != app.ConnectionDeviceBind && !state.AITask.Running)
	v.pointsButton.SetEnabled(state.Account != "" && state.Connection != app.ConnectionAuth && state.Connection != app.ConnectionDeviceBind && !state.PointsTask.Running && !state.RedeemTask.Running)
	v.redeemButton.SetEnabled(state.Account != "" && state.RedeemEnabled && !state.AutomationPaused && state.Connection != app.ConnectionAuth && state.Connection != app.ConnectionDeviceBind && !state.PointsTask.Running && !state.RedeemTask.Running)
	v.logoutButton.SetEnabled(state.Account != "" && !jobsRunning)

	connectionText, connectionColor := connectionStatusText(state.Connection)
	setWalkStatus(v.keepalive, homeStatusIndicatorText(connectionText), connectionColor)

	desktopName := state.DesktopName
	desktopColor := statusColorInfo
	if desktopName == "" {
		desktopName = "未选择"
		desktopColor = statusColorMuted
	}
	setWalkStatus(v.desktop, desktopName, desktopColor)
	setWalkStatus(v.points, fmt.Sprintf("%d", state.Points), statusColorInfo)
	setWalkText(v.pointsSync, pointsSyncText(state.PointsTask))

	redeemText, redeemColor := redeemHomeStatusText(state)
	setWalkStatus(v.redeem, homeStatusIndicatorText(redeemText), redeemColor)
	configuredDesktop := state.RedeemDesktopName
	if configuredDesktop == "" {
		configuredDesktop = "未配置"
	}
	setWalkText(v.redeemDesktop, configuredDesktop)
	configuredProduct := state.RedeemProductName
	if configuredProduct == "" {
		configuredProduct = "未配置"
	} else if state.RedeemCostPoints > 0 {
		configuredProduct = fmt.Sprintf("%s（%d 积分）", configuredProduct, state.RedeemCostPoints)
	}
	setWalkText(v.redeemProduct, configuredProduct)

	lastError := state.LastError
	if lastError == "" {
		lastError = "无"
	}
	setWalkText(v.lastError, lastError)

	loginAI, loginAIColor := pointsTaskStatusText(state.LoginAITask)
	setWalkStatus(v.loginAITask, homeStatusIndicatorText(loginAI), loginAIColor)
	usage, usageColor := usageTaskStatusText(state.UsageTask)
	setWalkStatus(v.usageTask, homeStatusIndicatorText(usage), usageColor)
	aiPoints, aiPointsColor := pointsTaskStatusText(state.AIPointsTask)
	setWalkStatus(v.aiPointsTask, homeStatusIndicatorText(aiPoints), aiPointsColor)
}

func setWalkText(label *walk.TextLabel, text string) {
	if label == nil {
		return
	}
	_ = label.SetText(text)
	label.SetTextColor(walk.Color(statusColorDefault))
}

func setWalkStatus(label *walk.TextLabel, text string, color uint32) {
	if label == nil {
		return
	}
	_ = label.SetText(text)
	label.SetTextColor(walk.Color(color))
}

func (v *walkMainView) runTask(name string, fn func() error) {
	go func() {
		if err := fn(); err != nil {
			walk.App().Synchronize(func() {
				walk.MsgBox(v.window, name, err.Error(), walk.MsgBoxIconError|walk.MsgBoxOK)
			})
		}
	}()
}

func (v *walkMainView) showMainWindow() {
	walk.App().Synchronize(func() {
		v.window.Show()
		_ = v.window.Activate()
	})
}

func (v *walkMainView) logout() {
	walk.App().Synchronize(func() {
		if walk.MsgBox(v.window, "退出账号", "确定退出当前账号并清除本地登录凭据吗？", walk.MsgBoxIconQuestion|walk.MsgBoxYesNo) != walk.DlgCmdYes {
			return
		}
		go func() {
			if err := v.runtime.Logout(); err != nil {
				walk.App().Synchronize(func() { walk.MsgBox(v.window, "退出账号", err.Error(), walk.MsgBoxIconError|walk.MsgBoxOK) })
			}
		}()
	})
}

func (v *walkMainView) quit() {
	walk.App().Synchronize(func() {
		v.mu.Lock()
		v.quitting = true
		v.mu.Unlock()
		walk.App().Exit(0)
	})
}
