package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/uvwt/CtyunHelper/internal/automation"
	"github.com/uvwt/CtyunHelper/internal/ctyun/auth"
)

func (f *AuthFlow) useProfile(profile auth.Profile) {
	f.client.UseProfile(profile)
	f.profileRevision.Add(1)
}

func (f *AuthFlow) clearProfile() {
	f.client.ClearProfile()
	f.profileRevision.Add(1)
}

func (f *AuthFlow) currentProfileRevision() uint64 {
	return f.profileRevision.Load()
}

func (f *AuthFlow) consumePendingLoginClaim(account string) bool {
	f.recoveryMu.Lock()
	defer f.recoveryMu.Unlock()
	if account == "" || f.pendingLoginClaimAccount != account {
		return false
	}
	f.pendingLoginClaimAccount = ""
	return true
}

func (f *AuthFlow) clearPendingLoginClaim() {
	f.recoveryMu.Lock()
	f.pendingLoginClaimAccount = ""
	f.recoveryMu.Unlock()
}

// RecoverExpiredProfile 只在调用方已经收到明确的 40010 后尝试一次同账号恢复。
// failedRevision 用来合并并发恢复：若另一个请求已经换入新 Profile，当前请求
// 直接复用新状态，不会再次提交登录。网络故障保持现状，下一轮任务仍可重试；
// 服务端明确拒绝登录（含验证码）才清理过期 Profile 并转人工处理。
func (f *AuthFlow) RecoverExpiredProfile(ctx context.Context, failedRevision uint64) error {
	f.recoveryMu.Lock()
	defer f.recoveryMu.Unlock()

	if f.profileRevision.Load() != failedRevision {
		state := f.model.Snapshot()
		switch state.Connection {
		case ConnectionAuth:
			return fmt.Errorf("app: 登录信息已过期，需要人工重新登录")
		case ConnectionDeviceBind:
			return fmt.Errorf("app: 当前设备需要重新绑定")
		default:
			return nil
		}
	}

	state := f.model.Snapshot()
	if state.Account == "" {
		message := "登录信息已过期，请重新登录"
		return errors.Join(fmt.Errorf("app: %s", message), f.invalidateExpiredProfile(message))
	}
	account, password, err := f.store.LoadLogin()
	if err != nil {
		return errors.Join(
			fmt.Errorf("app: 读取已保存登录凭据: %w", err),
			f.invalidateExpiredProfile("登录信息已过期，未找到可用于自动恢复的登录凭据，请重新登录"),
		)
	}
	if account != state.Account || password == "" {
		message := "登录信息已过期，已保存凭据与当前账号不一致，请重新登录"
		return errors.Join(fmt.Errorf("app: %s", message), f.invalidateExpiredProfile(message))
	}

	challenge, err := f.client.BeginLogin(ctx, account)
	if err != nil {
		return fmt.Errorf("app: 自动恢复登录 challenge: %w", err)
	}
	if f.guard != nil {
		if err := f.guard.Claim(automation.ActionLogin); err != nil {
			return fmt.Errorf("app: 自动恢复登录被保守策略阻止: %w", err)
		}
	}

	profile, err := f.client.Login(ctx, account, password, "", "", challenge)
	if err != nil {
		if _, serverRejected := auth.ErrorCode(err); serverRejected {
			f.pendingLoginClaimAccount = ""
			message := "登录信息已过期，自动恢复被服务端拒绝，请重新登录"
			if auth.RequiresLoginCaptcha(err) {
				message = "登录信息已过期，服务端要求图形验证码，请点击“账号登录”完成验证"
				// 这次自动登录已经占用了 Login Safety 额度。把同账号下一次人工无验证码
				// 提交视为同一登录流程的续步，避免为了触发验证码界面重复占用额度。
				if f.guard != nil {
					f.pendingLoginClaimAccount = account
				}
			} else if f.guard != nil {
				if safetyErr := f.guard.RecordFailure(); safetyErr != nil {
					err = errors.Join(err, safetyErr)
				}
			}
			return errors.Join(fmt.Errorf("app: %s: %w", message, err), f.invalidateExpiredProfile(message))
		}
		if f.guard != nil {
			if safetyErr := f.guard.RecordFailure(); safetyErr != nil {
				err = errors.Join(err, safetyErr)
			}
		}
		return fmt.Errorf("app: 自动恢复登录失败: %w", err)
	}

	if f.guard != nil {
		if err := f.guard.RecordSuccess(); err != nil {
			return fmt.Errorf("app: 保存自动恢复登录保护状态: %w", err)
		}
	}
	if err := f.store.SaveProfile(account, profile); err != nil {
		return fmt.Errorf("app: 保存自动恢复后的认证 Profile: %w", err)
	}
	f.pendingLoginClaimAccount = ""
	f.useProfile(profile)
	if !profile.BondedDevice {
		message := "登录已自动恢复，但当前 Windows 设备需要重新绑定"
		f.model.Update(func(state *State) {
			state.Connection = ConnectionDeviceBind
			state.LastError = message
		})
		return fmt.Errorf("app: %s", message)
	}
	f.model.Update(func(state *State) {
		state.LastError = ""
	})
	return nil
}

func (f *AuthFlow) invalidateExpiredProfile(message string) error {
	f.clearProfile()
	deleteErr := f.store.DeleteProfile()
	f.requireLogin(message)
	if deleteErr != nil {
		return fmt.Errorf("app: 清理失效 Profile: %w", deleteErr)
	}
	return nil
}
