package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/uvwt/CtyunHelper/internal/automation"
	"github.com/uvwt/CtyunHelper/internal/ctyun/auth"
)

type accountStore interface {
	SaveAccount(account string) error
	SaveLogin(account, password string) error
	LoadLogin() (account, password string, err error)
	DeleteLogin() error
	SaveProfile(account string, profile auth.Profile) error
	LoadProfile(account string) (auth.Profile, error)
	DeleteProfile() error
	DeleteClinkProfile() error
}

// AuthFlow 负责一个账号的登录态生命周期。它只在明确鉴权失效时清理缓存，
// 网络故障、节点故障不会触发重新登录，从源头避免登录风暴。
type AuthFlow struct {
	client *auth.Client
	store  accountStore
	model  *Model
	guard  *automation.Guard
}

func NewAuthFlow(client *auth.Client, store accountStore, model *Model, guard *automation.Guard) *AuthFlow {
	return &AuthFlow{client: client, store: store, model: model, guard: guard}
}

func (f *AuthFlow) Restore(account string) (bool, error) {
	if account == "" {
		f.requireLogin("")
		return false, nil
	}
	profile, err := f.store.LoadProfile(account)
	if errors.Is(err, os.ErrNotExist) {
		f.requireLogin("")
		return false, nil
	}
	if err != nil {
		f.requireLogin(err.Error())
		return false, err
	}
	f.client.UseProfile(profile)
	f.model.Update(func(state *State) {
		state.Account = account
		state.LoginAITask = PointsTaskStatus{}
		state.UsageTask = PointsTaskStatus{}
		state.AIPointsTask = PointsTaskStatus{}
		state.LastError = ""
		if profile.BondedDevice {
			state.Connection = ConnectionStopped
		} else {
			state.Connection = ConnectionDeviceBind
		}
	})
	return true, nil
}

func (f *AuthFlow) LoadStoredLogin() (account, password string, err error) {
	return f.store.LoadLogin()
}

func (f *AuthFlow) BeginLoginCaptcha(ctx context.Context, account string) (auth.LoginCaptcha, error) {
	if account == "" {
		return auth.LoginCaptcha{}, fmt.Errorf("app: 账号不能为空")
	}
	// 图形验证码只在服务端明确要求后获取；普通账号密码登录不会提前请求。
	return f.client.GetLoginCaptcha(ctx, account)
}

func (f *AuthFlow) CompleteLogin(ctx context.Context, account, password, captchaCode, captchaKey string) (auth.Profile, error) {
	if account == "" || password == "" {
		return auth.Profile{}, fmt.Errorf("app: 账号和密码不能为空")
	}
	// 官方客户端每次真正提交登录前都会重新获取 challenge。图形验证码是可选
	// 字段，不应和 challenge 绑定成“先取验证码再登录”的固定流程。
	challenge, err := f.client.BeginLogin(ctx, account)
	if err != nil {
		f.setLoginError(err)
		return auth.Profile{}, err
	}
	// 一次交互式登录流程只在最初的账号密码提交时占用一次登录额度。
	// 服务端要求验证码后的续步仍属于同一流程，不应重复消耗每日额度。
	if f.guard != nil && captchaCode == "" {
		if err := f.guard.Claim(automation.ActionLogin); err != nil {
			wrapped := fmt.Errorf("app: 登录被保守策略阻止: %w", err)
			f.setLoginError(wrapped)
			return auth.Profile{}, wrapped
		}
	}
	profile, err := f.client.Login(ctx, account, password, captchaCode, captchaKey, challenge)
	if err != nil {
		if f.guard != nil && !auth.RequiresLoginCaptcha(err) {
			if safetyErr := f.guard.RecordFailure(); safetyErr != nil {
				err = errors.Join(err, safetyErr)
			}
		}
		f.setLoginError(err)
		return auth.Profile{}, err
	}

	// 服务端登录成功后先把安全额度提交。失败时不写新账号凭据，也不覆盖当前 Profile。
	if f.guard != nil {
		if err := f.guard.RecordSuccess(); err != nil {
			f.setLoginError(err)
			return profile, fmt.Errorf("app: 保存登录保护状态: %w", err)
		}
	}
	if err := f.store.SaveLogin(account, password); err != nil {
		f.setLoginError(err)
		return auth.Profile{}, fmt.Errorf("app: 保存 Windows 凭据: %w", err)
	}
	if err := f.store.SaveProfile(account, profile); err != nil {
		f.setLoginError(err)
		return auth.Profile{}, fmt.Errorf("app: 保存认证 Profile: %w", err)
	}
	if err := f.store.SaveAccount(account); err != nil {
		f.setLoginError(err)
		return auth.Profile{}, fmt.Errorf("app: 保存账号配置: %w", err)
	}

	// 此时只完成“服务端登录 + 本地持久化”，仍不切换进程内当前 Profile。
	// Runtime 会先停止旧 Clink Session，再调用 CommitLogin 原子提交新账号。
	return profile, nil
}

func (f *AuthFlow) CommitLogin(account string, profile auth.Profile) {
	f.client.UseProfile(profile)
	f.model.Update(func(state *State) {
		state.Account = account
		state.LoginAITask = PointsTaskStatus{}
		state.UsageTask = PointsTaskStatus{}
		state.AIPointsTask = PointsTaskStatus{}
		state.LastError = ""
		if profile.BondedDevice {
			state.Connection = ConnectionStopped
		} else {
			state.Connection = ConnectionDeviceBind
		}
	})
}

func (f *AuthFlow) setLoginError(err error) {
	// 更换账号失败只属于候选登录流程。只要当前 Profile 仍有效，就让旧账号和
	// 旧 Clink 会话继续工作；登录窗口本身会直接展示候选流程错误。
	if _, ok := f.client.Profile(); ok {
		return
	}
	f.model.Update(func(state *State) {
		state.Connection = ConnectionAuth
		state.LastError = err.Error()
	})
}

func (f *AuthFlow) BeginDeviceBinding(ctx context.Context) (auth.DeviceBindingChallenge, error) {
	challenge, err := f.client.BeginDeviceBinding(ctx)
	if err != nil {
		f.setAuthError(err)
	}
	return challenge, err
}

func (f *AuthFlow) SendDeviceSMS(ctx context.Context, captchaCode, captchaKey string) (string, error) {
	smsKey, err := f.client.SendDeviceSMS(ctx, captchaCode, captchaKey)
	if err != nil {
		f.setAuthError(err)
	}
	return smsKey, err
}

func (f *AuthFlow) CompleteDeviceBinding(ctx context.Context, smsCode, smsKey string) error {
	profile, err := f.client.BindDevice(ctx, smsCode, smsKey)
	if err != nil {
		f.setAuthError(err)
		return err
	}
	account := f.model.Snapshot().Account
	if err := f.store.SaveProfile(account, profile); err != nil {
		f.setAuthError(err)
		return fmt.Errorf("app: 保存绑定后的 Profile: %w", err)
	}
	f.client.UseProfile(profile)
	f.model.Update(func(state *State) {
		state.Connection = ConnectionStopped
		state.LastError = ""
	})
	return nil
}

// HandleSessionError 只处理需要改变登录态的错误；调用方仍然保留原始错误用于日志/状态。
func (f *AuthFlow) HandleSessionError(err error) error {
	if auth.RequiresDeviceBinding(err) {
		f.model.Update(func(state *State) {
			state.Connection = ConnectionDeviceBind
			state.LastError = err.Error()
		})
		return nil
	}
	if !auth.RequiresAuthentication(err) {
		return nil
	}
	f.client.ClearProfile()
	if deleteErr := f.store.DeleteProfile(); deleteErr != nil {
		return fmt.Errorf("app: 清理失效 Profile: %w", deleteErr)
	}
	f.requireLogin(err.Error())
	return nil
}

func (f *AuthFlow) Logout() error {
	f.client.ClearProfile()
	cleanupErr := errors.Join(
		f.store.DeleteProfile(),
		f.store.DeleteClinkProfile(),
		f.store.DeleteLogin(),
		f.store.SaveAccount(""),
	)
	message := ""
	if cleanupErr != nil {
		message = cleanupErr.Error()
	}
	// 当前进程的认证态已经被主动清空，即使某个持久化删除动作失败，也不能
	// 继续把旧账号/旧积分显示成“仍已登录”。错误保留给 UI，用户可再次退出
	// 或人工处理磁盘/凭据问题；下一次启动也不会把内存 Profile 复活。
	f.model.Update(func(state *State) {
		state.Account = ""
		state.DesktopID = ""
		state.DesktopName = ""
		state.OnlineSince = time.Time{}
		state.Points = 0
		state.LoginAITask = PointsTaskStatus{}
		state.UsageTask = PointsTaskStatus{}
		state.AIPointsTask = PointsTaskStatus{}
		state.Connection = ConnectionAuth
		state.LastError = message
	})
	return cleanupErr
}

func (f *AuthFlow) setAuthError(err error) {
	if auth.RequiresDeviceBinding(err) {
		f.model.Update(func(state *State) {
			state.Connection = ConnectionDeviceBind
			state.LastError = err.Error()
		})
		return
	}
	if auth.RequiresAuthentication(err) {
		f.model.Update(func(state *State) {
			state.Connection = ConnectionAuth
			state.LastError = err.Error()
		})
		return
	}
	// 普通网络/协议错误不代表登录态失效。尤其在设备绑定阶段，不能因为
	// 一次验证码请求超时就把用户从“需要绑定设备”错误踢回登录页。
	f.model.Update(func(state *State) {
		state.LastError = err.Error()
	})
}

func (f *AuthFlow) requireLogin(message string) {
	f.model.Update(func(state *State) {
		state.Connection = ConnectionAuth
		state.LastError = message
	})
}
