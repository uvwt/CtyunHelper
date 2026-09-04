package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/uvwt/CtyunHelper/internal/ctyun/auth"
	"github.com/uvwt/CtyunHelper/internal/logging"
)

type SessionRunner interface {
	Run(context.Context) error
}

type RuntimeOptions struct {
	RedeemSettings *RedeemSettingsService
	Settings       *SettingsService
	Logger         *logging.Logger
}

// Runtime 是 UI 唯一需要调用的 App 命令入口。它负责登录流程和一个长期保活会话，
// 并保证同一进程不会并发启动两个 Clink Session。
type Runtime struct {
	model      *Model
	auth       *AuthFlow
	session    SessionRunner
	automation *TaskAutomation
	redeem     *RedeemSettingsService
	settings   *SettingsService
	logger     *logging.Logger

	mu            sync.Mutex
	rootCtx       context.Context
	rootCancel    context.CancelFunc
	sessionCancel context.CancelFunc
	sessionDone   chan struct{}
	sessionActive bool
}

func NewRuntime(model *Model, authFlow *AuthFlow, session SessionRunner, taskAutomation *TaskAutomation, options RuntimeOptions) *Runtime {
	runtime := &Runtime{
		model: model, auth: authFlow, session: session, automation: taskAutomation,
		redeem: options.RedeemSettings, settings: options.Settings, logger: options.Logger,
	}
	if runtime.logger != nil && model != nil {
		runtime.logger.SetOnEntry(func(entry logging.Entry) {
			model.Events().Publish(Event{Type: EventLogAdded, Data: entry})
		})
	}
	return runtime
}

func (r *Runtime) Model() *Model {
	return r.model
}

func (r *Runtime) CurrentSettings() (GeneralSettings, error) {
	if r.settings == nil {
		return GeneralSettings{}, fmt.Errorf("app: 通用设置未初始化")
	}
	return r.settings.Current()
}

func (r *Runtime) SaveSettings(settings GeneralSettings) error {
	if r.settings == nil {
		return fmt.Errorf("app: 通用设置未初始化")
	}
	return r.settings.Save(settings)
}

func (r *Runtime) LogSnapshot(limit int) []logging.Entry {
	if r.logger == nil {
		return nil
	}
	return r.logger.Snapshot(limit)
}

func (r *Runtime) LogPath() string {
	if r.logger == nil {
		return ""
	}
	return r.logger.Path()
}

func (r *Runtime) CurrentRedeemSettings() (RedeemSettingsView, error) {
	if r.redeem == nil {
		return RedeemSettingsView{}, fmt.Errorf("app: 兑换设置未初始化")
	}
	return r.redeem.Current()
}

func (r *Runtime) LoadRedeemCatalog(ctx context.Context) (RedeemCatalog, error) {
	if r.redeem == nil {
		return RedeemCatalog{}, fmt.Errorf("app: 兑换设置未初始化")
	}
	return r.redeem.Catalog(ctx)
}

func (r *Runtime) SaveRedeemSettings(ctx context.Context, request SaveRedeemSettingsRequest) error {
	if r.redeem == nil {
		return fmt.Errorf("app: 兑换设置未初始化")
	}
	return r.redeem.Save(ctx, request)
}

func (r *Runtime) ResolvePendingRedeem(succeeded bool) error {
	if r.redeem == nil {
		return fmt.Errorf("app: 兑换设置未初始化")
	}
	return r.redeem.ResolvePending(succeeded)
}

func (r *Runtime) RunAITask() error {
	if r.automation == nil {
		return fmt.Errorf("app: AI 自动任务未初始化")
	}
	if r.model.Snapshot().AutomationPaused {
		return fmt.Errorf("app: AI 自动任务已暂停")
	}
	r.mu.Lock()
	ctx := r.rootCtx
	r.mu.Unlock()
	if ctx == nil || ctx.Err() != nil {
		return fmt.Errorf("app: Runtime 尚未启动或已经停止")
	}
	return r.automation.RunAI(ctx)
}

func (r *Runtime) RunPointsTask() error {
	if r.automation == nil {
		return fmt.Errorf("app: 积分刷新任务未初始化")
	}
	state := r.model.Snapshot()
	if state.Connection == ConnectionAuth || state.Connection == ConnectionDeviceBind {
		return fmt.Errorf("app: 请先完成登录和设备绑定")
	}
	r.mu.Lock()
	ctx := r.rootCtx
	r.mu.Unlock()
	if ctx == nil || ctx.Err() != nil {
		return fmt.Errorf("app: Runtime 尚未启动或已经停止")
	}
	return r.automation.RunPoints(ctx)
}

func (r *Runtime) RunRedeemTask() error {
	if r.automation == nil {
		return fmt.Errorf("app: 兑换任务未初始化")
	}
	state := r.model.Snapshot()
	if state.AutomationPaused {
		return fmt.Errorf("app: 自动任务已暂停")
	}
	if state.Connection == ConnectionAuth || state.Connection == ConnectionDeviceBind {
		return fmt.Errorf("app: 请先完成登录和设备绑定")
	}
	if !state.RedeemEnabled {
		return fmt.Errorf("app: 自动兑换未启用")
	}
	r.mu.Lock()
	ctx := r.rootCtx
	r.mu.Unlock()
	if ctx == nil || ctx.Err() != nil {
		return fmt.Errorf("app: Runtime 尚未启动或已经停止")
	}
	return r.automation.RunRedeem(ctx)
}

func (r *Runtime) Start(parent context.Context) {
	r.mu.Lock()
	if r.rootCtx != nil {
		r.mu.Unlock()
		return
	}
	r.rootCtx, r.rootCancel = context.WithCancel(parent)
	rootCtx := r.rootCtx
	r.mu.Unlock()
	if r.logger != nil && r.model != nil {
		events, unsubscribe := r.model.Events().Subscribe(64)
		go r.observeState(rootCtx, r.model.Snapshot(), events, unsubscribe)
		r.logger.Info("app", "Runtime 启动")
	}
	if r.automation != nil {
		r.automation.Start(rootCtx)
	}
	r.StartSession()
}

func (r *Runtime) Stop() {
	if r.logger != nil {
		r.logger.Info("app", "Runtime 停止")
	}
	r.mu.Lock()
	if r.sessionCancel != nil {
		r.sessionCancel()
	}
	if r.rootCancel != nil {
		r.rootCancel()
	}
	r.mu.Unlock()
	if r.logger != nil {
		_ = r.logger.Close()
	}
}

func (r *Runtime) StartSession() {
	if r.auth == nil || r.auth.client == nil || r.session == nil {
		return
	}
	profile, ok := r.auth.client.Profile()
	if !ok || !profile.BondedDevice {
		return
	}
	r.mu.Lock()
	if r.rootCtx == nil || r.rootCtx.Err() != nil || r.sessionActive {
		r.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(r.rootCtx)
	done := make(chan struct{})
	r.sessionCancel = cancel
	r.sessionDone = done
	r.sessionActive = true
	r.mu.Unlock()
	go r.runSession(ctx, done)
}

func (r *Runtime) runSession(ctx context.Context, done chan struct{}) {
	defer func() {
		r.mu.Lock()
		if r.sessionDone == done {
			r.sessionActive = false
			r.sessionCancel = nil
			r.sessionDone = nil
		}
		r.mu.Unlock()
		close(done)
	}()
	for {
		err := r.session.Run(ctx)
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			return
		}
		if err == nil {
			return
		}
		_ = r.auth.HandleSessionError(err)
		state := r.model.Snapshot().Connection
		if state == ConnectionAuth || state == ConnectionDeviceBind {
			return
		}
		timer := time.NewTimer(30 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (r *Runtime) Restore(account string) (bool, error) {
	restored, err := r.auth.Restore(account)
	if r.automation != nil {
		if restored && err == nil {
			r.automation.UpdateAccount(account)
		} else {
			r.automation.UpdateAccount("")
		}
	}
	return restored, err
}

func (r *Runtime) LoadStoredLogin() (account, password string, err error) {
	return r.auth.LoadStoredLogin()
}

func (r *Runtime) BeginLogin(ctx context.Context, account string) (auth.LoginChallenge, error) {
	return r.auth.BeginLogin(ctx, account)
}

func (r *Runtime) CompleteLogin(ctx context.Context, account, password, captchaCode string, challenge auth.LoginChallenge) (auth.Profile, error) {
	if r.automation != nil {
		if !r.automation.activityMu.TryLock() {
			return auth.Profile{}, fmt.Errorf("app: 自动任务正在运行，暂不能更换账号")
		}
		defer r.automation.activityMu.Unlock()
	}
	// AI Job 会在一次执行中连续使用积分接口和 EAI；运行中切换 Profile 会让
	// 前后请求落到不同账号。账号切换属于低频人工操作，直接拒绝比中途取消
	// 自动任务更容易保持整条业务链的一致性。
	if state := r.model.Snapshot(); state.AITask.Running || state.PointsTask.Running || state.RedeemTask.Running {
		return auth.Profile{}, fmt.Errorf("app: 自动任务正在运行，暂不能更换账号")
	}
	profile, err := r.auth.CompleteLogin(ctx, account, password, captchaCode, challenge)
	if err != nil {
		return auth.Profile{}, err
	}

	// 更换账号后必须先等旧 Clink Session 完整退出，再允许新账号建立连接。
	// 不能只替换 Profile 后直接 StartSession，否则旧会话仍在线时新会话会被
	// sessionActive 拦住，最终形成“界面是新账号、连接还是旧账号”的错位状态。
	if err := r.stopSession(ctx); err != nil {
		return profile, fmt.Errorf("app: 停止旧云电脑会话: %w", err)
	}
	r.auth.CommitLogin(account, profile)
	if r.automation != nil {
		r.automation.UpdateAccount(account)
	}
	if profile.BondedDevice {
		r.StartSession()
	}
	return profile, nil
}

func (r *Runtime) BeginDeviceBinding(ctx context.Context) (auth.DeviceBindingChallenge, error) {
	return r.auth.BeginDeviceBinding(ctx)
}

func (r *Runtime) SendDeviceSMS(ctx context.Context, captchaCode, captchaKey string) (string, error) {
	return r.auth.SendDeviceSMS(ctx, captchaCode, captchaKey)
}

func (r *Runtime) CompleteDeviceBinding(ctx context.Context, smsCode, smsKey string) error {
	if r.automation != nil {
		if !r.automation.activityMu.TryLock() {
			return fmt.Errorf("app: 自动任务正在运行，暂不能完成设备绑定")
		}
		defer r.automation.activityMu.Unlock()
	}
	if err := r.auth.CompleteDeviceBinding(ctx, smsCode, smsKey); err != nil {
		return err
	}
	r.StartSession()
	return nil
}

func (r *Runtime) Logout() error {
	if r.automation != nil {
		if !r.automation.activityMu.TryLock() {
			return fmt.Errorf("app: 自动任务正在运行，暂不能退出账号")
		}
		defer r.automation.activityMu.Unlock()
	}
	state := r.model.Snapshot()
	if state.AITask.Running || state.PointsTask.Running || state.RedeemTask.Running {
		return fmt.Errorf("app: 自动任务正在运行，暂不能退出账号")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.stopSession(ctx); err != nil {
		return fmt.Errorf("app: 停止云电脑会话: %w", err)
	}
	logoutErr := r.auth.Logout()
	// AuthFlow 会在本地清理部分失败时仍清空当前进程认证态；兑换计划也必须
	// 同步切到“无账号”，不能因为 Credential/磁盘错误而继续显示为可执行。
	if r.automation != nil {
		r.automation.UpdateAccount("")
	}
	return logoutErr
}

func (r *Runtime) stopSession(ctx context.Context) error {
	r.mu.Lock()
	cancel := r.sessionCancel
	done := r.sessionDone
	r.mu.Unlock()
	if cancel == nil || done == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
