package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uvwt/CtyunHelper/internal/ctyun/auth"
)

type blockingSession struct {
	starts atomic.Int32
}

func (s *blockingSession) Run(ctx context.Context) error {
	s.starts.Add(1)
	<-ctx.Done()
	return ctx.Err()
}

type switchingSession struct {
	starts    atomic.Int32
	active    atomic.Int32
	maxActive atomic.Int32
	started   chan int32
}

func (s *switchingSession) Run(ctx context.Context) error {
	start := s.starts.Add(1)
	active := s.active.Add(1)
	for {
		max := s.maxActive.Load()
		if active <= max || s.maxActive.CompareAndSwap(max, active) {
			break
		}
	}
	s.started <- start
	<-ctx.Done()
	s.active.Add(-1)
	return ctx.Err()
}

func TestRuntimeStartsOnlyOneSessionForRestoredBoundProfile(t *testing.T) {
	profile := auth.Profile{UserID: 1, SecretKey: "test", CommonLoginReqHeader: "common", BondedDevice: true}
	store := &memoryAccountStore{profile: profile, profileExists: true}
	client := auth.NewClient(auth.DeviceIdentity{Code: "ctyun_fixed"}, auth.ClientOptions{})
	model := NewModel(State{})
	flow := NewAuthFlow(client, store, model, nil)
	if restored, err := flow.Restore("account"); err != nil || !restored {
		t.Fatalf("restore=%v err=%v", restored, err)
	}
	session := &blockingSession{}
	runtime := NewRuntime(model, flow, session, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime.Start(ctx)
	runtime.StartSession()
	runtime.StartSession()
	deadline := time.Now().Add(time.Second)
	for session.starts.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := session.starts.Load(); got != 1 {
		t.Fatalf("session starts = %d, want 1", got)
	}
	runtime.Stop()
}

func TestRuntimeDoesNotStartUnboundSession(t *testing.T) {
	profile := auth.Profile{UserID: 1, SecretKey: "test", CommonLoginReqHeader: "common", BondedDevice: false}
	store := &memoryAccountStore{profile: profile, profileExists: true}
	client := auth.NewClient(auth.DeviceIdentity{Code: "ctyun_fixed"}, auth.ClientOptions{})
	client.UseProfile(profile)
	model := NewModel(State{Connection: ConnectionDeviceBind})
	flow := NewAuthFlow(client, store, model, nil)
	session := &blockingSession{}
	runtime := NewRuntime(model, flow, session, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime.Start(ctx)
	time.Sleep(20 * time.Millisecond)
	if got := session.starts.Load(); got != 0 {
		t.Fatalf("unbound session starts = %d", got)
	}
	runtime.Stop()
}

func TestRuntimeSwitchAccountStopsOldSessionBeforeStartingNewOne(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/client/login" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"userId": 2, "userEid": "new-eid", "tenantId": 3,
				"secretKey": "new-key", "commonLoginReqHeader": "new-common",
				"bondedDevice": true,
			},
		})
	}))
	defer server.Close()

	oldProfile := auth.Profile{
		UserID: 1, UserEID: "old-eid", TenantID: 2,
		SecretKey: "old-key", CommonLoginReqHeader: "old-common", BondedDevice: true,
	}
	store := &memoryAccountStore{account: "old-account", profile: oldProfile, profileExists: true}
	client := auth.NewClient(auth.DeviceIdentity{Code: "ctyun_fixed"}, auth.ClientOptions{
		APIOrigin: server.URL, HTTPClient: server.Client(),
	})
	client.UseProfile(oldProfile)
	model := NewModel(State{Account: "old-account", Connection: ConnectionStopped})
	flow := NewAuthFlow(client, store, model, nil)
	session := &switchingSession{started: make(chan int32, 4)}
	runtime := NewRuntime(model, flow, session, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime.Start(ctx)

	select {
	case start := <-session.started:
		if start != 1 {
			t.Fatalf("first start = %d", start)
		}
	case <-time.After(time.Second):
		t.Fatal("old session did not start")
	}

	challenge := auth.LoginChallenge{ID: "challenge", Code: "salt", CaptchaKey: "captcha-key"}
	if _, err := runtime.CompleteLogin(context.Background(), "new-account", "password", "1234", challenge); err != nil {
		t.Fatal(err)
	}
	select {
	case start := <-session.started:
		if start != 2 {
			t.Fatalf("second start = %d", start)
		}
	case <-time.After(time.Second):
		t.Fatal("new session did not start")
	}
	if got := session.maxActive.Load(); got != 1 {
		t.Fatalf("max concurrent sessions = %d, want 1", got)
	}
	if got := model.Snapshot().Account; got != "new-account" {
		t.Fatalf("account = %q", got)
	}
	runtime.Stop()
}

func TestRuntimeRejectsAccountSwitchWhileAIJobIsRunning(t *testing.T) {
	model := NewModel(State{Account: "old-account", AITask: JobStatus{Running: true}})
	runtime := NewRuntime(model, nil, nil, nil)
	_, err := runtime.CompleteLogin(
		context.Background(),
		"new-account",
		"password",
		"1234",
		auth.LoginChallenge{ID: "challenge", Code: "salt"},
	)
	if err == nil || err.Error() != "app: 自动任务正在运行，暂不能更换账号" {
		t.Fatalf("CompleteLogin() error = %v", err)
	}
	if got := model.Snapshot().Account; got != "old-account" {
		t.Fatalf("account changed to %q", got)
	}
}
