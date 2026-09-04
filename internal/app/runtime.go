package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/uvwt/CtyunHelper/internal/ctyun/auth"
)

type SessionRunner interface {
	Run(context.Context) error
}

// Runtime 是 UI 唯一需要调用的 App 命令入口。它负责登录流程和一个长期保活会话，
// 并保证同一进程不会并发启动两个 Clink Session。
type Runtime struct {
	model      *Model
	auth       *AuthFlow
	session    SessionRunner
	automation *TaskAutomation

	mu            sync.Mutex
	rootCtx       context.Context
	rootCancel    context.CancelFunc
	sessionCancel context.CancelFunc
	sessionDone   chan struct{}
	sessionActive bool
}

func NewRuntime(model *Model, authFlow *AuthFlow, session SessionRunner, taskAutomation *TaskAutomation) *Runtime {
	return &Runtime{model: model, auth: authFlow, session: session, automation: taskAutomation}
}

func (r *Runtime) Model() *Model {
	return r.model
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

func (r *Runtime) Start(parent context.Context) {
	r.mu.Lock()
	if r.rootCtx != nil {
		r.mu.Unlock()
		return
	}
	r.rootCtx, r.rootCancel = context.WithCancel(parent)
	rootCtx := r.rootCtx
	r.mu.Unlock()
	if r.automation != nil {
		r.automation.Start(rootCtx)
	}
	r.StartSession()
}

func (r *Runtime) Stop() {
	r.mu.Lock()
	if r.sessionCancel != nil {
		r.sessionCancel()
	}
	if r.rootCancel != nil {
		r.rootCancel()
	}
	r.mu.Unlock()
}

func (r *Runtime) StartSession() {
	profile, ok := r.auth.client.Profile()
	if !ok || !profile.BondedDevice || r.session == nil {
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
	return r.auth.Restore(account)
}

func (r *Runtime) LoadStoredLogin() (account, password string, err error) {
	return r.auth.LoadStoredLogin()
}

func (r *Runtime) BeginLogin(ctx context.Context, account string) (auth.LoginChallenge, error) {
	return r.auth.BeginLogin(ctx, account)
}

func (r *Runtime) CompleteLogin(ctx context.Context, account, password, captchaCode string, challenge auth.LoginChallenge) (auth.Profile, error) {
	// AI Job 会在一次执行中连续使用积分接口和 EAI；运行中切换 Profile 会让
	// 前后请求落到不同账号。账号切换属于低频人工操作，直接拒绝比中途取消
	// 自动任务更容易保持整条业务链的一致性。
	if r.model.Snapshot().AITask.Running {
		return auth.Profile{}, fmt.Errorf("app: AI 任务正在运行，暂不能更换账号")
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
	if err := r.auth.CompleteDeviceBinding(ctx, smsCode, smsKey); err != nil {
		return err
	}
	r.StartSession()
	return nil
}

func (r *Runtime) Logout() error {
	r.mu.Lock()
	if r.sessionCancel != nil {
		r.sessionCancel()
	}
	r.mu.Unlock()
	return r.auth.Logout()
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
