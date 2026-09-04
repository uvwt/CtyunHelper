package app

import (
	"context"
	"errors"
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
	model   *Model
	auth    *AuthFlow
	session SessionRunner

	mu            sync.Mutex
	rootCtx       context.Context
	rootCancel    context.CancelFunc
	sessionCancel context.CancelFunc
	sessionActive bool
}

func NewRuntime(model *Model, authFlow *AuthFlow, session SessionRunner) *Runtime {
	return &Runtime{model: model, auth: authFlow, session: session}
}

func (r *Runtime) Model() *Model {
	return r.model
}

func (r *Runtime) Start(parent context.Context) {
	r.mu.Lock()
	if r.rootCtx != nil {
		r.mu.Unlock()
		return
	}
	r.rootCtx, r.rootCancel = context.WithCancel(parent)
	r.mu.Unlock()
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
	r.sessionCancel = cancel
	r.sessionActive = true
	r.mu.Unlock()
	go r.runSession(ctx)
}

func (r *Runtime) runSession(ctx context.Context) {
	defer func() {
		r.mu.Lock()
		r.sessionActive = false
		r.sessionCancel = nil
		r.mu.Unlock()
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

func (r *Runtime) BeginLogin(ctx context.Context, account string) (auth.LoginChallenge, error) {
	return r.auth.BeginLogin(ctx, account)
}

func (r *Runtime) CompleteLogin(ctx context.Context, account, password, captchaCode string, challenge auth.LoginChallenge) (auth.Profile, error) {
	profile, err := r.auth.CompleteLogin(ctx, account, password, captchaCode, challenge)
	if err == nil && profile.BondedDevice {
		r.StartSession()
	}
	return profile, err
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
