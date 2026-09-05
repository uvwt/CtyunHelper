package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/uvwt/CtyunHelper/internal/ctyun/auth"
	"github.com/uvwt/CtyunHelper/internal/ctyun/desktop"
)

type fakeDesktopService struct {
	values     []desktop.Desktop
	connection desktop.ConnectionInfo
	listErr    error
	connectErr error
	resolvedID string
}

func (f *fakeDesktopService) ListForKeepalive(context.Context) ([]desktop.Desktop, error) {
	return f.values, f.listErr
}

func (f *fakeDesktopService) ResolveClinkConnection(_ context.Context, value desktop.Desktop) (desktop.ConnectionInfo, error) {
	f.resolvedID = value.ID()
	return f.connection, f.connectErr
}

type fakeClinkAuthStore struct {
	profile auth.Profile
}

func (s *fakeClinkAuthStore) LoadLogin() (string, string, error) {
	return "", "", errors.New("unexpected Clink refresh")
}

func (s *fakeClinkAuthStore) SaveClinkProfile(string, auth.Profile) error { return nil }

func (s *fakeClinkAuthStore) LoadClinkProfile(string) (auth.Profile, error) {
	return s.profile, nil
}

func (s *fakeClinkAuthStore) DeleteClinkProfile() error { return nil }

func newKeepaliveForTest(primary *auth.Client, desktops desktopService, model *Model) *Keepalive {
	clinkClient := auth.NewClient(auth.DeviceIdentity{Code: "device-code"}, auth.ClientOptions{})
	store := &fakeClinkAuthStore{profile: auth.Profile{
		UserID: 123, UserName: "tester", TenantID: 456,
		SecretKey: "secret", CommonLoginReqHeader: "common",
	}}
	return &Keepalive{
		primaryAuth: primary,
		clinkAuth:   newClinkAuthFlow(clinkClient, store, nil),
		desktops:    desktops,
		model:       model,
	}
}

func TestKeepaliveRunsDesktopResolveAndRealClinkWorker(t *testing.T) {
	authClient := auth.NewClient(auth.DeviceIdentity{Code: "device-code"}, auth.ClientOptions{})
	authClient.UseProfile(auth.Profile{UserID: 123, UserName: "tester"})
	desktops := &fakeDesktopService{
		values: []desktop.Desktop{
			{DesktopID: "blocked", DesktopName: "禁用桌面", UseStatus: "25", Forbidden: true},
			{DesktopID: "7", DesktopName: "测试云电脑", UseStatus: "25"},
		},
		connection: desktop.ConnectionInfo{
			DesktopID:       7,
			Host:            "desktop.internal",
			Port:            "7033",
			ClinkLVSOutHost: "127.0.0.1:1",
		},
	}
	model := NewModel(State{Account: "account"})
	keepalive := newKeepaliveForTest(authClient, desktops, model)

	events, unsubscribe := model.Events().Subscribe(16)
	defer unsubscribe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- keepalive.Run(ctx) }()

	deadline := time.After(3 * time.Second)
waitBackoff:
	for {
		select {
		case event := <-events:
			state, ok := event.Data.(State)
			if ok && state.Connection == ConnectionBackoff && state.LastError != "" {
				break waitBackoff
			}
		case <-deadline:
			t.Fatal("keepalive did not enter Clink backoff state")
		}
	}
	if desktops.resolvedID != "7" {
		t.Fatalf("resolved desktop = %q", desktops.resolvedID)
	}
	connected := model.Snapshot()
	if connected.DesktopID != "7" || connected.DesktopName != "测试云电脑" {
		t.Fatalf("connected state = %#v", connected)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() after cancellation = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("keepalive did not stop")
	}
	final := model.Snapshot()
	if final.Connection != ConnectionStopped || !final.OnlineSince.IsZero() {
		t.Fatalf("final state = %#v", final)
	}
}

func TestKeepaliveRequiresAuthenticatedProfile(t *testing.T) {
	authClient := auth.NewClient(auth.DeviceIdentity{Code: "device-code"}, auth.ClientOptions{})
	model := NewModel(State{})
	keepalive := newKeepaliveForTest(authClient, &fakeDesktopService{}, model)
	err := keepalive.Run(context.Background())
	if err == nil {
		t.Fatalf("Run() error = %v", err)
	}
	if model.Snapshot().Connection != ConnectionAuth {
		t.Fatalf("state = %#v", model.Snapshot())
	}
}

func TestKeepaliveKeepsLegacyProtocolErrorsOutOfPrimaryAuthState(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want ConnectionState
	}{
		{name: "auth", err: auth.APIError{Code: auth.CodeNoPermissions, Message: "expired"}, want: ConnectionBackoff},
		{name: "device binding", err: auth.APIError{Code: auth.CodeDeviceUnbound, Message: "unbind"}, want: ConnectionBackoff},
		{name: "network", err: context.DeadlineExceeded, want: ConnectionBackoff},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authClient := auth.NewClient(auth.DeviceIdentity{Code: "device-code"}, auth.ClientOptions{})
			authClient.UseProfile(auth.Profile{UserID: 123})
			model := NewModel(State{})
			keepalive := newKeepaliveForTest(authClient, &fakeDesktopService{listErr: tt.err}, model)
			if err := keepalive.Run(context.Background()); err == nil {
				t.Fatal("Run() expected error")
			}
			if got := model.Snapshot().Connection; got != tt.want {
				t.Fatalf("state = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestSelectDesktopSkipsStoppedAndForbidden(t *testing.T) {
	got, ok := selectDesktop([]desktop.Desktop{
		{DesktopID: "1", UseStatusText: "关机"},
		{DesktopID: "2", UseStatus: "25", Forbidden: true},
		{DesktopID: "3", DesktopName: "在线", UseStatusText: "运行中"},
	})
	if !ok || got.ID() != "3" {
		t.Fatalf("selected = %#v, ok=%v", got, ok)
	}
}
