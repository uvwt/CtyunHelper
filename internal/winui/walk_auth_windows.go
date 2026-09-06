//go:build windows

package winui

import (
	"context"
	"strings"
	"time"

	"github.com/tailscale/walk"
	d "github.com/tailscale/walk/declarative"
	"github.com/uvwt/CtyunHelper/internal/ctyun/auth"
)

func (v *walkMainView) openLogin() {
	var (
		dlg            *walk.Dialog
		accountEdit    *walk.LineEdit
		passwordEdit   *walk.LineEdit
		captchaPanel   *walk.Composite
		captchaView    *walk.ImageView
		captchaEdit    *walk.LineEdit
		noteLabel      *walk.Label
		loginButton    *walk.PushButton
		refreshButton  *walk.PushButton
		cancelButton   *walk.PushButton
		captchaKey     string
		captchaAccount string
		captchaBitmap  *walk.Bitmap
		busy           bool
		needsBinding   bool
	)

	setBusy := func(value bool) {
		busy = value
		if loginButton != nil {
			loginButton.SetEnabled(!value)
		}
		if refreshButton != nil {
			refreshButton.SetEnabled(!value)
		}
	}

	var loadCaptcha func()
	loadCaptcha = func() {
		if busy {
			return
		}
		account := strings.TrimSpace(accountEdit.Text())
		if account == "" {
			walk.MsgBox(dlg, "登录", "请先填写账号。", walk.MsgBoxIconWarning|walk.MsgBoxOK)
			return
		}
		setBusy(true)
		_ = noteLabel.SetText("正在获取图形验证码…")
		captchaPanel.SetVisible(true)
		accountEdit.SetEnabled(false)
		passwordEdit.SetEnabled(false)
		go func(account string) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			captcha, err := v.runtime.BeginLoginCaptcha(ctx, account)
			walk.App().Synchronize(func() {
				setBusy(false)
				if err != nil {
					walk.MsgBox(dlg, "验证码", err.Error(), walk.MsgBoxIconError|walk.MsgBoxOK)
					return
				}
				captchaKey = captcha.Key
				captchaAccount = account
				if err := setWalkCaptchaImage(captchaView, captcha.Image, &captchaBitmap); err != nil {
					walk.MsgBox(dlg, "验证码", err.Error(), walk.MsgBoxIconError|walk.MsgBoxOK)
					return
				}
				_ = captchaEdit.SetText("")
				_ = noteLabel.SetText("服务端要求图形验证码。输入后再次登录；错误或过期时可刷新。")
			})
		}(account)
	}

	submit := func() {
		if busy {
			return
		}
		account := strings.TrimSpace(accountEdit.Text())
		password := passwordEdit.Text()
		if account == "" || password == "" {
			walk.MsgBox(dlg, "登录", "请输入账号和密码。", walk.MsgBoxIconWarning|walk.MsgBoxOK)
			return
		}
		captchaCode := ""
		if captchaPanel.Visible() {
			if captchaAccount != account || captchaKey == "" {
				loadCaptcha()
				return
			}
			captchaCode = strings.TrimSpace(captchaEdit.Text())
			if captchaCode == "" {
				walk.MsgBox(dlg, "登录", "请输入图形验证码。", walk.MsgBoxIconWarning|walk.MsgBoxOK)
				return
			}
		}

		setBusy(true)
		_ = noteLabel.SetText("正在登录…")
		go func(account, password, captchaCode, captchaKey string) {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			profile, err := v.runtime.CompleteLogin(ctx, account, password, captchaCode, captchaKey)
			walk.App().Synchronize(func() {
				setBusy(false)
				if err != nil {
					if auth.RequiresLoginCaptcha(err) {
						_ = noteLabel.SetText("服务端要求图形验证码，正在刷新…")
						loadCaptcha()
						return
					}
					walk.MsgBox(dlg, "登录失败", err.Error(), walk.MsgBoxIconError|walk.MsgBoxOK)
					return
				}
				needsBinding = !profile.BondedDevice
				dlg.Accept()
			})
		}(account, password, captchaCode, captchaKey)
	}

	if err := (d.Dialog{
		AssignTo:  &dlg,
		Title:     "登录天翼云电脑",
		Size:      d.Size{Width: 460, Height: 410},
		MinSize:   d.Size{Width: 420, Height: 320},
		Layout:    d.VBox{Margins: d.Margins{Left: 12, Top: 12, Right: 12, Bottom: 12}, Spacing: 8},
		FixedSize: false,
		Children: []d.Widget{
			d.Label{Text: "正常情况下只需账号和密码；服务端要求时会自动显示图形验证码。", EllipsisMode: d.EllipsisNone},
			d.Composite{Layout: d.Grid{Columns: 2, Spacing: 8}, Children: []d.Widget{
				d.Label{Text: "账号", MinSize: d.Size{Width: 80}},
				d.LineEdit{AssignTo: &accountEdit, StretchFactor: 1},
				d.Label{Text: "密码", MinSize: d.Size{Width: 80}},
				d.LineEdit{AssignTo: &passwordEdit, PasswordMode: true, StretchFactor: 1},
			}},
			d.Composite{AssignTo: &captchaPanel, Visible: false, Layout: d.VBox{MarginsZero: true, Spacing: 6}, Children: []d.Widget{
				d.Composite{Layout: d.HBox{MarginsZero: true, Spacing: 8}, Children: []d.Widget{
					d.Label{Text: "图形验证码", MinSize: d.Size{Width: 80}},
					d.ImageView{AssignTo: &captchaView, MinSize: d.Size{Width: 120, Height: 56}, MaxSize: d.Size{Width: 160, Height: 64}, Mode: d.ImageViewModeShrink},
					d.PushButton{AssignTo: &refreshButton, Text: "刷新", OnClicked: func() { loadCaptcha() }},
					d.HSpacer{},
				}},
				d.Composite{Layout: d.HBox{MarginsZero: true, Spacing: 8}, Children: []d.Widget{
					d.Label{Text: "验证码", MinSize: d.Size{Width: 80}},
					d.LineEdit{AssignTo: &captchaEdit, StretchFactor: 1},
				}},
			}},
			d.Label{AssignTo: &noteLabel, Text: "请输入账号和密码。", EllipsisMode: d.EllipsisNone},
			d.HSpacer{},
			d.Composite{Layout: d.HBox{MarginsZero: true, Spacing: 8}, Children: []d.Widget{
				d.HSpacer{},
				d.PushButton{AssignTo: &loginButton, Text: "登录", OnClicked: submit},
				d.PushButton{AssignTo: &cancelButton, Text: "取消", OnClicked: func() { dlg.Cancel() }},
			}},
		},
		DefaultButton: &loginButton,
		CancelButton:  &cancelButton,
	}).Create(v.window); err != nil {
		walk.MsgBox(v.window, "登录", err.Error(), walk.MsgBoxIconError|walk.MsgBoxOK)
		return
	}
	defer dlg.Dispose()
	defer func() {
		if captchaBitmap != nil {
			captchaBitmap.Dispose()
		}
	}()

	if icon, err := walk.NewIconFromResourceId(1); err == nil {
		_ = dlg.SetIcon(icon)
	}
	if account, password, err := v.runtime.LoadStoredLogin(); err == nil {
		_ = accountEdit.SetText(account)
		_ = passwordEdit.SetText(password)
	} else {
		_ = accountEdit.SetText(v.model.Snapshot().Account)
	}

	dlg.Run()
	if needsBinding {
		v.openBinding()
	}
}

func (v *walkMainView) openBinding() {
	var (
		dlg           *walk.Dialog
		mobileLabel   *walk.Label
		captchaView   *walk.ImageView
		captchaEdit   *walk.LineEdit
		smsEdit       *walk.LineEdit
		sendButton    *walk.PushButton
		bindButton    *walk.PushButton
		cancelButton  *walk.PushButton
		captchaBitmap *walk.Bitmap
		captchaKey    string
		smsKey        string
		busy          bool
	)

	setBusy := func(value bool) {
		busy = value
		if sendButton != nil {
			sendButton.SetEnabled(!value)
		}
		if bindButton != nil {
			bindButton.SetEnabled(!value)
		}
	}

	loadChallenge := func() {
		if busy {
			return
		}
		setBusy(true)
		_ = mobileLabel.SetText("正在获取设备验证码…")
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			challenge, err := v.runtime.BeginDeviceBinding(ctx)
			walk.App().Synchronize(func() {
				setBusy(false)
				if err != nil {
					walk.MsgBox(dlg, "设备绑定", err.Error(), walk.MsgBoxIconError|walk.MsgBoxOK)
					return
				}
				captchaKey = challenge.CaptchaKey
				_ = mobileLabel.SetText("短信验证号码：" + challenge.Mobile)
				if err := setWalkCaptchaImage(captchaView, challenge.Captcha, &captchaBitmap); err != nil {
					walk.MsgBox(dlg, "设备绑定", err.Error(), walk.MsgBoxIconError|walk.MsgBoxOK)
				}
			})
		}()
	}

	sendSMS := func() {
		if busy {
			return
		}
		code := strings.TrimSpace(captchaEdit.Text())
		if code == "" || captchaKey == "" {
			walk.MsgBox(dlg, "设备绑定", "请先获取并填写图形验证码。", walk.MsgBoxIconWarning|walk.MsgBoxOK)
			return
		}
		setBusy(true)
		go func(code, key string) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			newSMSKey, err := v.runtime.SendDeviceSMS(ctx, code, key)
			walk.App().Synchronize(func() {
				setBusy(false)
				if err != nil {
					walk.MsgBox(dlg, "发送短信失败", err.Error(), walk.MsgBoxIconError|walk.MsgBoxOK)
					loadChallenge()
					return
				}
				smsKey = newSMSKey
				walk.MsgBox(dlg, "设备绑定", "短信验证码已发送。", walk.MsgBoxIconInformation|walk.MsgBoxOK)
			})
		}(code, captchaKey)
	}

	complete := func() {
		if busy {
			return
		}
		code := strings.TrimSpace(smsEdit.Text())
		if code == "" || smsKey == "" {
			walk.MsgBox(dlg, "设备绑定", "请先发送短信并填写短信验证码。", walk.MsgBoxIconWarning|walk.MsgBoxOK)
			return
		}
		setBusy(true)
		go func(code, key string) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			err := v.runtime.CompleteDeviceBinding(ctx, code, key)
			walk.App().Synchronize(func() {
				setBusy(false)
				if err != nil {
					walk.MsgBox(dlg, "设备绑定失败", err.Error(), walk.MsgBoxIconError|walk.MsgBoxOK)
					return
				}
				walk.MsgBox(dlg, "设备绑定", "设备绑定成功，正在启动云电脑保活。", walk.MsgBoxIconInformation|walk.MsgBoxOK)
				dlg.Accept()
			})
		}(code, smsKey)
	}

	if err := (d.Dialog{
		AssignTo: &dlg,
		Title:    "绑定当前 Windows 设备",
		Size:     d.Size{Width: 470, Height: 410},
		MinSize:  d.Size{Width: 430, Height: 350},
		Layout:   d.VBox{Margins: d.Margins{Left: 12, Top: 12, Right: 12, Bottom: 12}, Spacing: 8},
		Children: []d.Widget{
			d.Label{AssignTo: &mobileLabel, Text: "正在获取设备验证码…", EllipsisMode: d.EllipsisNone},
			d.Composite{Layout: d.HBox{MarginsZero: true, Spacing: 8}, Children: []d.Widget{
				d.Label{Text: "图形验证码", MinSize: d.Size{Width: 86}},
				d.ImageView{AssignTo: &captchaView, MinSize: d.Size{Width: 120, Height: 56}, MaxSize: d.Size{Width: 160, Height: 64}, Mode: d.ImageViewModeShrink},
				d.HSpacer{},
			}},
			d.Composite{Layout: d.HBox{MarginsZero: true, Spacing: 8}, Children: []d.Widget{
				d.Label{Text: "验证码", MinSize: d.Size{Width: 86}},
				d.LineEdit{AssignTo: &captchaEdit, StretchFactor: 1},
				d.PushButton{AssignTo: &sendButton, Text: "发送短信", OnClicked: sendSMS},
			}},
			d.Composite{Layout: d.HBox{MarginsZero: true, Spacing: 8}, Children: []d.Widget{
				d.Label{Text: "短信验证码", MinSize: d.Size{Width: 86}},
				d.LineEdit{AssignTo: &smsEdit, StretchFactor: 1},
			}},
			d.Label{Text: "设备码首次生成后固定保存；本程序不会静默更换设备身份。", EllipsisMode: d.EllipsisNone},
			d.HSpacer{},
			d.Composite{Layout: d.HBox{MarginsZero: true, Spacing: 8}, Children: []d.Widget{
				d.HSpacer{},
				d.PushButton{AssignTo: &bindButton, Text: "完成绑定", OnClicked: complete},
				d.PushButton{AssignTo: &cancelButton, Text: "取消", OnClicked: func() { dlg.Cancel() }},
			}},
		},
		DefaultButton: &bindButton,
		CancelButton:  &cancelButton,
	}).Create(v.window); err != nil {
		walk.MsgBox(v.window, "设备绑定", err.Error(), walk.MsgBoxIconError|walk.MsgBoxOK)
		return
	}
	defer dlg.Dispose()
	defer func() {
		if captchaBitmap != nil {
			captchaBitmap.Dispose()
		}
	}()
	if icon, err := walk.NewIconFromResourceId(1); err == nil {
		_ = dlg.SetIcon(icon)
	}

	loadChallenge()
	dlg.Run()
}

func setWalkCaptchaImage(view *walk.ImageView, raw []byte, current **walk.Bitmap) error {
	decoded, err := decodeCaptchaImage(raw)
	if err != nil {
		return err
	}
	bitmap, err := walk.NewBitmapFromImage(decoded)
	if err != nil {
		return err
	}
	if err := view.SetImage(bitmap); err != nil {
		bitmap.Dispose()
		return err
	}
	if *current != nil {
		(*current).Dispose()
	}
	*current = bitmap
	return nil
}
