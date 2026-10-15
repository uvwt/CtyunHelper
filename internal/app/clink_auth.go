package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/uvwt/CtyunHelper/internal/automation"
	"github.com/uvwt/CtyunHelper/internal/ctyun/auth"
)

var errClinkLoginActionRequired = errors.New("Clink 登录需要人工处理")

type clinkAuthStore interface {
	LoadLogin() (account, password string, err error)
	SaveClinkProfile(account string, profile auth.Profile) error
	LoadClinkProfile(account string) (auth.Profile, error)
	DeleteClinkProfile() error
}

// clinkAuthFlow 维护旧 Clink 协议自己的登录态。
// 现代 Windows Profile 与该 Profile 的 deviceType/version 不同，服务端明确拒绝混用。
type clinkAuthFlow struct {
	client *auth.Client
	store  clinkAuthStore
	guard  *automation.Guard
}

func newClinkAuthFlow(client *auth.Client, store clinkAuthStore, guard *automation.Guard) *clinkAuthFlow {
	return &clinkAuthFlow{client: client, store: store, guard: guard}
}

func (f *clinkAuthFlow) restoreOrRefresh(ctx context.Context, account string) (auth.Profile, error) {
	if account == "" {
		return auth.Profile{}, fmt.Errorf("app: Clink 登录缺少当前账号")
	}
	if profile, err := f.store.LoadClinkProfile(account); err == nil {
		f.client.UseProfile(profile)
		return profile, nil
	}
	return f.refresh(ctx, account)
}

func (f *clinkAuthFlow) refresh(ctx context.Context, account string) (auth.Profile, error) {
	storedAccount, password, err := f.store.LoadLogin()
	if err != nil {
		return auth.Profile{}, fmt.Errorf("app: 读取 Clink 登录凭据: %w", err)
	}
	if storedAccount != account || password == "" {
		return auth.Profile{}, fmt.Errorf("app: Clink 登录凭据与当前账号不一致")
	}
	if f.guard != nil {
		if err := f.guard.Claim(automation.ActionClinkLogin); err != nil {
			return auth.Profile{}, fmt.Errorf("app: Clink 登录被保守策略阻止: %w", err)
		}
	}

	profile, err := f.client.LoginLegacyClink(ctx, storedAccount, password)
	if err != nil {
		if auth.RequiresLoginCaptcha(err) {
			// legacy Clink 登录没有验证码续步能力。51030/51031/51032/51040
			// 都意味着自动重试无法自行恢复，应保留现代 Profile 并等待用户重新登录。
			return auth.Profile{}, fmt.Errorf(
				"%w: Clink 登录凭据已失效或服务端要求人工验证，请重新登录天翼云电脑: %v",
				errClinkLoginActionRequired,
				err,
			)
		}
		if f.guard != nil {
			if safetyErr := f.guard.RecordFailure(); safetyErr != nil {
				return auth.Profile{}, fmt.Errorf("app: Clink 登录失败: %v; 保存失败状态: %w", err, safetyErr)
			}
		}
		return auth.Profile{}, fmt.Errorf("app: Clink 登录失败: %w", err)
	}
	if f.guard != nil {
		if err := f.guard.RecordSuccess(); err != nil {
			return auth.Profile{}, fmt.Errorf("app: 保存 Clink 登录保护状态: %w", err)
		}
	}
	if err := f.store.SaveClinkProfile(account, profile); err != nil {
		return auth.Profile{}, fmt.Errorf("app: 保存 Clink Profile: %w", err)
	}
	f.client.UseProfile(profile)
	return profile, nil
}

func (f *clinkAuthFlow) invalidate() error {
	f.client.ClearProfile()
	if err := f.store.DeleteClinkProfile(); err != nil {
		return fmt.Errorf("app: 清理失效 Clink Profile: %w", err)
	}
	return nil
}
