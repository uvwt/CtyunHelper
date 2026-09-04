package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/uvwt/CtyunHelper/internal/ctyun/auth"
	"github.com/uvwt/CtyunHelper/internal/ctyun/clink"
	"github.com/uvwt/CtyunHelper/internal/ctyun/desktop"
)

type desktopService interface {
	List(context.Context) ([]desktop.Desktop, error)
	ResolveConnection(context.Context, desktop.Desktop) (desktop.ConnectionInfo, error)
}

// Keepalive 将已认证 Profile、云电脑发现/connect 与 Clink Worker 串成一条长期主链。
// 登录/验证码由 App 的认证流程负责；Keepalive 不读取密码，也不决定何时重登。
type Keepalive struct {
	auth     *auth.Client
	desktops desktopService
	model    *Model
}

func NewKeepalive(authClient *auth.Client, desktops *desktop.Client, model *Model) *Keepalive {
	return &Keepalive{auth: authClient, desktops: desktops, model: model}
}

func (k *Keepalive) Run(ctx context.Context) error {
	profile, ok := k.auth.Profile()
	if !ok {
		k.setError(ConnectionAuth, "需要先登录天翼云电脑")
		return fmt.Errorf("app: 尚未登录")
	}
	values, err := k.desktops.List(ctx)
	if err != nil {
		k.applyProtocolError(err)
		return err
	}
	selected, ok := selectDesktop(values)
	if !ok {
		err := fmt.Errorf("app: 没有可连接且正在运行的云电脑")
		k.setError(ConnectionError, err.Error())
		return err
	}
	k.model.Update(func(state *State) {
		state.DesktopID = selected.ID()
		state.DesktopName = selected.Name()
		state.Connection = ConnectionConnecting
		state.LastError = ""
	})

	connection, err := k.desktops.ResolveConnection(ctx, selected)
	if err != nil {
		k.applyProtocolError(err)
		return err
	}
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

func (k *Keepalive) applyProtocolError(err error) {
	state := ConnectionBackoff
	if auth.RequiresDeviceBinding(err) {
		state = ConnectionDeviceBind
	} else if auth.RequiresAuthentication(err) {
		state = ConnectionAuth
	}
	k.setError(state, err.Error())
}

func (k *Keepalive) setError(state ConnectionState, message string) {
	k.model.Update(func(current *State) {
		current.Connection = state
		current.LastError = message
	})
}
