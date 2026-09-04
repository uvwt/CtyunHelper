package app

import (
	"context"
	"errors"
	"fmt"
	"os"

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
}

// AuthFlow 负责一个账号的登录态生命周期。它只在明确鉴权失效时清理缓存，
// 网络故障、节点故障不会触发重新登录，从源头避免登录风暴。
type AuthFlow struct {
	client *auth.Client
	store  accountStore
	model  *Model
}

func NewAuthFlow(client *auth.Client, store accountStore, model *Model) *AuthFlow {
	return &AuthFlow{client: client, store: store, model: model}
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

func (f *AuthFlow) BeginLogin(ctx context.Context, account string) (auth.LoginChallenge, error) {
	if account == "" {
		return auth.LoginChallenge{}, fmt.Errorf("app: 账号不能为空")
	}
	f.model.Update(func(state *State) {
		state.Account = account
		state.Connection = ConnectionAuth
		state.LastError = ""
	})
	challenge, err := f.client.BeginLogin(ctx, account)
	if err != nil {
		f.setAuthError(err)
	}
	return challenge, err
}

func (f *AuthFlow) CompleteLogin(ctx context.Context, account, password, captchaCode string, challenge auth.LoginChallenge) (auth.Profile, error) {
	profile, err := f.client.Login(ctx, account, password, captchaCode, challenge)
	if err != nil {
		f.setAuthError(err)
		return auth.Profile{}, err
	}
	if err := f.store.SaveLogin(account, password); err != nil {
		f.setAuthError(err)
		return auth.Profile{}, fmt.Errorf("app: 保存 Windows 凭据: %w", err)
	}
	if err := f.store.SaveProfile(account, profile); err != nil {
		f.setAuthError(err)
		return auth.Profile{}, fmt.Errorf("app: 保存认证 Profile: %w", err)
	}
	if err := f.store.SaveAccount(account); err != nil {
		f.setAuthError(err)
		return auth.Profile{}, fmt.Errorf("app: 保存账号配置: %w", err)
	}
	f.model.Update(func(state *State) {
		state.Account = account
		state.LastError = ""
		if profile.BondedDevice {
			state.Connection = ConnectionStopped
		} else {
			state.Connection = ConnectionDeviceBind
		}
	})
	return profile, nil
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
	if err := f.client.BindDevice(ctx, smsCode, smsKey); err != nil {
		f.setAuthError(err)
		return err
	}
	profile, ok := f.client.Profile()
	if !ok {
		return fmt.Errorf("app: 设备绑定后认证 Profile 丢失")
	}
	account := f.model.Snapshot().Account
	if err := f.store.SaveProfile(account, profile); err != nil {
		f.setAuthError(err)
		return fmt.Errorf("app: 保存绑定后的 Profile: %w", err)
	}
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
	if err := f.store.DeleteProfile(); err != nil {
		return err
	}
	if err := f.store.DeleteLogin(); err != nil {
		return err
	}
	f.requireLogin("")
	return nil
}

func (f *AuthFlow) setAuthError(err error) {
	if auth.RequiresDeviceBinding(err) {
		f.model.Update(func(state *State) {
			state.Connection = ConnectionDeviceBind
			state.LastError = err.Error()
		})
		return
	}
	f.model.Update(func(state *State) {
		state.Connection = ConnectionAuth
		state.LastError = err.Error()
	})
}

func (f *AuthFlow) requireLogin(message string) {
	f.model.Update(func(state *State) {
		state.Connection = ConnectionAuth
		state.LastError = message
	})
}
