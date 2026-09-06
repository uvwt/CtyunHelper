//go:build windows

package winui

import (
	"fmt"
	"strings"

	"github.com/tailscale/walk"
	d "github.com/tailscale/walk/declarative"
	"github.com/uvwt/CtyunHelper/internal/app"
	"github.com/uvwt/CtyunHelper/internal/buildinfo"
)

func (v *walkMainView) openSettings() {
	settings, err := v.runtime.CurrentSettings()
	if err != nil {
		walk.MsgBox(v.window, "设置", err.Error(), walk.MsgBoxIconError|walk.MsgBoxOK)
		return
	}

	var (
		dlg               *walk.Dialog
		automationCheck   *walk.CheckBox
		startOnLoginCheck *walk.CheckBox
		saveButton        *walk.PushButton
		cancelButton      *walk.PushButton
	)

	save := func() {
		if err := v.runtime.SaveSettings(app.GeneralSettings{
			AutomationEnabled: automationCheck.Checked(),
			StartOnLogin:      startOnLoginCheck.Checked(),
		}); err != nil {
			walk.MsgBox(dlg, "设置", err.Error(), walk.MsgBoxIconError|walk.MsgBoxOK)
			return
		}
		dlg.Accept()
	}

	if err := (d.Dialog{
		AssignTo: &dlg,
		Title:    "设置",
		Size:     d.Size{Width: 420, Height: 250},
		MinSize:  d.Size{Width: 380, Height: 220},
		Layout:   d.VBox{Margins: d.Margins{Left: 14, Top: 14, Right: 14, Bottom: 14}, Spacing: 10},
		Children: []d.Widget{
			d.GroupBox{Title: "运行设置", Layout: d.VBox{Spacing: 8}, Children: []d.Widget{
				d.CheckBox{AssignTo: &automationCheck, Text: "启用自动任务"},
				d.CheckBox{AssignTo: &startOnLoginCheck, Text: "登录 Windows 后自动启动"},
			}},
			d.HSpacer{},
			d.Composite{Layout: d.HBox{MarginsZero: true, Spacing: 8}, Children: []d.Widget{
				d.HSpacer{},
				d.PushButton{AssignTo: &saveButton, Text: "保存", OnClicked: save},
				d.PushButton{AssignTo: &cancelButton, Text: "取消", OnClicked: func() { dlg.Cancel() }},
			}},
		},
		DefaultButton: &saveButton,
		CancelButton:  &cancelButton,
	}).Create(v.window); err != nil {
		walk.MsgBox(v.window, "设置", err.Error(), walk.MsgBoxIconError|walk.MsgBoxOK)
		return
	}
	defer dlg.Dispose()
	if icon, err := walk.NewIconFromResourceId(1); err == nil {
		_ = dlg.SetIcon(icon)
	}
	automationCheck.SetChecked(settings.AutomationEnabled)
	startOnLoginCheck.SetChecked(settings.StartOnLogin)
	dlg.Run()
}

func (v *walkMainView) openLogs() {
	var (
		dlg           *walk.Dialog
		logEdit       *walk.TextEdit
		pathLabel     *walk.Label
		refreshButton *walk.PushButton
		closeButton   *walk.PushButton
	)

	refresh := func() {
		entries := v.runtime.LogSnapshot(500)
		lines := make([]string, 0, len(entries))
		for _, entry := range entries {
			lines = append(lines, entry.Line())
		}
		_ = logEdit.SetText(strings.Join(lines, "\r\n"))
		_ = pathLabel.SetText("日志文件：" + v.runtime.LogPath())
	}

	if err := (d.Dialog{
		AssignTo: &dlg,
		Title:    "日志",
		Size:     d.Size{Width: 720, Height: 500},
		MinSize:  d.Size{Width: 520, Height: 360},
		Layout:   d.VBox{Margins: d.Margins{Left: 10, Top: 10, Right: 10, Bottom: 10}, Spacing: 8},
		Children: []d.Widget{
			d.Label{AssignTo: &pathLabel, EllipsisMode: d.EllipsisPath, StretchFactor: 1},
			d.TextEdit{AssignTo: &logEdit, ReadOnly: true, VScroll: true, HScroll: true, StretchFactor: 1},
			d.Composite{Layout: d.HBox{MarginsZero: true, Spacing: 8}, Children: []d.Widget{
				d.PushButton{AssignTo: &refreshButton, Text: "刷新", OnClicked: refresh},
				d.HSpacer{},
				d.PushButton{AssignTo: &closeButton, Text: "关闭", OnClicked: func() { dlg.Accept() }},
			}},
		},
		CancelButton: &closeButton,
	}).Create(v.window); err != nil {
		walk.MsgBox(v.window, "日志", err.Error(), walk.MsgBoxIconError|walk.MsgBoxOK)
		return
	}
	defer dlg.Dispose()
	if icon, err := walk.NewIconFromResourceId(1); err == nil {
		_ = dlg.SetIcon(icon)
	}
	refresh()
	dlg.Run()
}

func (v *walkMainView) openAbout() {
	var (
		dlg         *walk.Dialog
		closeButton *walk.PushButton
	)
	info := fmt.Sprintf("%s\r\n\r\n版本：%s\r\n作者：%s\r\n\r\n%s",
		buildinfo.DisplayName, buildinfo.Version, buildinfo.Author, buildinfo.RepositoryURL)

	if err := (d.Dialog{
		AssignTo: &dlg,
		Title:    "关于",
		Size:     d.Size{Width: 430, Height: 280},
		MinSize:  d.Size{Width: 390, Height: 250},
		Layout:   d.VBox{Margins: d.Margins{Left: 16, Top: 16, Right: 16, Bottom: 16}, Spacing: 10},
		Children: []d.Widget{
			d.Label{Text: info, EllipsisMode: d.EllipsisNone, StretchFactor: 1},
			d.HSpacer{},
			d.Composite{Layout: d.HBox{MarginsZero: true, Spacing: 8}, Children: []d.Widget{
				d.PushButton{Text: "打开项目主页", OnClicked: func() {
					if err := openAboutRepository(uintptr(dlg.Handle())); err != nil {
						walk.MsgBox(dlg, "关于", err.Error(), walk.MsgBoxIconError|walk.MsgBoxOK)
					}
				}},
				d.HSpacer{},
				d.PushButton{AssignTo: &closeButton, Text: "关闭", OnClicked: func() { dlg.Accept() }},
			}},
		},
		CancelButton: &closeButton,
	}).Create(v.window); err != nil {
		walk.MsgBox(v.window, "关于", err.Error(), walk.MsgBoxIconError|walk.MsgBoxOK)
		return
	}
	defer dlg.Dispose()
	if icon, err := walk.NewIconFromResourceId(1); err == nil {
		_ = dlg.SetIcon(icon)
	}
	dlg.Run()
}

func closeWalkDialogs() {
	// Walk 对话框均为主窗口拥有的模态窗口，Run 返回后立即 Dispose；
	// 主消息循环退出时没有独立 HWND 生命周期需要额外清理。
}
