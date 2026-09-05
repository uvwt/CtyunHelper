package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/uvwt/CtyunHelper/internal/automation"
	"github.com/uvwt/CtyunHelper/internal/ctyun/auth"
	"github.com/uvwt/CtyunHelper/internal/ctyun/clink"
	"github.com/uvwt/CtyunHelper/internal/ctyun/desktop"
)

type desktopService interface {
	ListForKeepalive(context.Context) ([]desktop.Desktop, error)
	ResolveClinkConnection(context.Context, desktop.Desktop) (desktop.ConnectionInfo, error)
}

// Keepalive 将主登录态、Clink 专用登录态、云电脑发现/connect 与 Worker 串成一条长期主链。
// 现代 Profile 只决定当前账号是否可运行；真正的 Clink 请求始终使用独立 legacy Profile。
type Keepalive struct {
	primaryAuth *auth.Client
	clinkAuth   *clinkAuthFlow
	desktops    desktopService
	model       *Model
}

func NewKeepalive(primaryAuth, clinkClient *auth.Client, desktops *desktop.Client, store clinkAuthStore, guard *automation.Guard, model *Model) *Keepalive {
	return &Keepalive{
		primaryAuth: primaryAuth,
		clinkAuth:   newClinkAuthFlow(clinkClient, store, guard),
		desktops:    desktops,
		model:       model,
	}
}

func (k *Keepalive) Run(ctx context.Context) error {
	if k.primaryAuth == nil || k.clinkAuth == nil {
		return fmt.Errorf("app: Clink 保活未初始化")
	}
	_, ok := k.primaryAuth.Profile()
	if !ok {
		k.setError(ConnectionAuth, "需要先登录天翼云电脑")
		return fmt.Errorf("app: 尚未登录")
	}
	account := k.model.Snapshot().Account
	profile, err := k.clinkAuth.restoreOrRefresh(ctx, account)
	if err != nil {
		return k.failClinkAuth(err)
	}

	selected, connection, err := k.resolveClinkConnection(ctx)
	if auth.RequiresAuthentication(err) {
		// 40010 只表示 Clink legacy Profile 已失效。它不能反向清理现代 Profile，
		// 否则两代认证态会再次互相污染。清掉独立缓存后只刷新一次并重试整条路由发现。
		if invalidateErr := k.clinkAuth.invalidate(); invalidateErr != nil {
			return k.failClinkAuth(invalidateErr)
		}
		profile, err = k.clinkAuth.refresh(ctx, account)
		if err != nil {
			return k.failClinkAuth(err)
		}
		selected, connection, err = k.resolveClinkConnection(ctx)
	}
	if err != nil {
		return k.failClinkProtocol(err)
	}
	k.model.Update(func(state *State) {
		state.DesktopID = selected.ID()
		state.DesktopName = selected.Name()
		state.Connection = ConnectionConnecting
		state.LastError = ""
	})
	worker := clink.NewWorker(clink.WorkerConfig{
		Connection: connection,
		UserID:     profile.UserID,
		UserName:   profile.UserName,
	}, k.applyClinkSnapshot)
	if err := worker.Run(ctx); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			k.model.Update(func(state *State) {
				state.Connection = ConnectionStopped
				state.OnlineSince = time.Time{}
				state.LastError = ""
			})
			return nil
		}
		k.setError(ConnectionBackoff, err.Error())
		return err
	}
	return nil
}

func (k *Keepalive) resolveClinkConnection(ctx context.Context) (desktop.Desktop, desktop.ConnectionInfo, error) {
	values, err := k.desktops.ListForKeepalive(ctx)
	if err != nil {
		return desktop.Desktop{}, desktop.ConnectionInfo{}, err
	}
	selected, ok := selectDesktop(values)
	if !ok {
		return desktop.Desktop{}, desktop.ConnectionInfo{}, fmt.Errorf("app: 没有可连接且正在运行的云电脑")
	}
	connection, err := k.desktops.ResolveClinkConnection(ctx, selected)
	if err != nil {
		return desktop.Desktop{}, desktop.ConnectionInfo{}, err
	}
	return selected, connection, nil
}

func selectDesktop(values []desktop.Desktop) (desktop.Desktop, bool) {
	for _, value := range values {
		if value.Running() && !value.Forbidden {
			return value, true
		}
	}
	return desktop.Desktop{}, false
}

func (k *Keepalive) applyClinkSnapshot(snapshot clink.Snapshot) {
	k.model.Update(func(state *State) {
		state.OnlineSince = snapshot.OnlineSince
		state.LastError = snapshot.LastError
		switch snapshot.State {
		case clink.StateResolving, clink.StateConnecting, clink.StateHandshaking:
			state.Connection = ConnectionConnecting
		case clink.StateOnline:
			state.Connection = ConnectionOnline
		case clink.StateBackoff:
			state.Connection = ConnectionBackoff
		case clink.StatePaused:
			state.Connection = ConnectionPaused
		case clink.StateAuthRequired:
			state.Connection = ConnectionAuth
		case clink.StateFatal:
			state.Connection = ConnectionError
		case clink.StateStopped, clink.StateIdle:
			state.Connection = ConnectionStopped
		}
	})
}

func (k *Keepalive) failClinkAuth(err error) error {
	message := fmt.Sprintf("Clink 专用登录失败: %v", err)
	k.setError(ConnectionBackoff, message)
	// 故意不 %w：Runtime 只允许现代 Profile 改变主登录态，不能把 legacy 40010
	// 重新解释成“主登录过期”。原始错误已经完整保留在状态和日志文本中。
	return fmt.Errorf("app: %s", message)
}

func (k *Keepalive) failClinkProtocol(err error) error {
	message := fmt.Sprintf("Clink 路由请求失败: %v", err)
	k.setError(ConnectionBackoff, message)
	return fmt.Errorf("app: %s", message)
}

func (k *Keepalive) setError(state ConnectionState, message string) {
	k.model.Update(func(current *State) {
		current.Connection = state
		current.LastError = message
	})
}
