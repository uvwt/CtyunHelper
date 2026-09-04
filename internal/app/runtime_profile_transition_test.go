package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/uvwt/CtyunHelper/internal/automation"
	"github.com/uvwt/CtyunHelper/internal/ctyun/auth"
	"github.com/uvwt/CtyunHelper/internal/ctyun/points"
)

type profileAwareSession struct {
	client    *auth.Client
	started   chan struct{}
	oldAtStop chan bool
}

func (s *profileAwareSession) Run(ctx context.Context) error {
	select {
	case s.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	profile, ok := s.client.Profile()
	s.oldAtStop <- ok && profile.UserID == 1
	return ctx.Err()
}

func TestRuntimeStopsOldSessionBeforeCommittingNewProfile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/client/login" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"userId": 2, "userEid": "new-eid", "tenantId": 3,
				"secretKey": "new-key", "commonLoginReqHeader": "new-common", "bondedDevice": true,
			},
		})
	}))
	defer server.Close()

	old := auth.Profile{UserID: 1, UserEID: "old", TenantID: 2, SecretKey: "old-key", CommonLoginReqHeader: "old-common", BondedDevice: true}
	client := auth.NewClient(auth.DeviceIdentity{Code: "ctyun_fixed"}, auth.ClientOptions{APIOrigin: server.URL, HTTPClient: server.Client()})
	client.UseProfile(old)
	model := NewModel(State{Account: "old-account", Connection: ConnectionStopped})
	flow := NewAuthFlow(client, &memoryAccountStore{account: "old-account", profile: old, profileExists: true}, model, nil)
	session := &profileAwareSession{client: client, started: make(chan struct{}, 1), oldAtStop: make(chan bool, 1)}
	runtime := NewRuntime(model, flow, session, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime.Start(ctx)
	select {
	case <-session.started:
	case <-time.After(time.Second):
		t.Fatal("old session did not start")
	}

	challenge := auth.LoginChallenge{ID: "challenge", Code: "salt", CaptchaKey: "captcha-key"}
	if _, err := runtime.CompleteLogin(context.Background(), "new-account", "password", "1234", challenge); err != nil {
		t.Fatal(err)
	}
	select {
	case oldAtStop := <-session.oldAtStop:
		if !oldAtStop {
			t.Fatal("old Clink session observed new profile before it stopped")
		}
	case <-time.After(time.Second):
		t.Fatal("old session did not stop")
	}
	profile, ok := client.Profile()
	if !ok || profile.UserID != 2 || model.Snapshot().Account != "new-account" {
		t.Fatalf("new profile/model not committed: profile=%#v ok=%v state=%#v", profile, ok, model.Snapshot())
	}
	runtime.Stop()
}

type blockingPointsClient struct {
	started chan struct{}
	release chan struct{}
}

func (c *blockingPointsClient) Tasks(ctx context.Context) ([]points.Task, error) {
	select {
	case c.started <- struct{}{}:
	default:
	}
	select {
	case <-c.release:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (c *blockingPointsClient) GeneralPoints(context.Context) (int, error) { return 0, nil }

func TestRuntimeAccountSwitchCannotRaceRunningPointsOperation(t *testing.T) {
	model := NewModel(State{Account: "old-account", Connection: ConnectionOnline})
	guard := automation.NewGuard(automation.DefaultPolicy(), automation.SafetyState{}, automation.GuardOptions{Now: time.Now})
	aiJob := automation.NewAIJob(&appAIPoints{tasks: []points.Task{{Name: automation.AITaskName, Status: automation.TaskDone}}}, &appAIChat{}, guard, "你好")
	blocking := &blockingPointsClient{started: make(chan struct{}, 1), release: make(chan struct{})}
	pointsJob := automation.NewPointsJob(blocking, automation.PointsJobOptions{})
	tasks, err := NewTaskAutomationWithOptions(model, TaskAutomationOptions{AIJob: aiJob, PointsJob: pointsJob})
	if err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(model, nil, nil, tasks, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime.Start(ctx)

	// Start() 本身会触发一次只读积分刷新，等它真正进入协议调用后再尝试换号。
	select {
	case <-blocking.started:
	case <-time.After(time.Second):
		t.Fatal("points operation did not start")
	}
	_, err = runtime.CompleteLogin(context.Background(), "new-account", "password", "1234", auth.LoginChallenge{ID: "x", Code: "y"})
	if err == nil || err.Error() != "app: 自动任务正在运行，暂不能更换账号" {
		t.Fatalf("CompleteLogin() error = %v", err)
	}
	if model.Snapshot().Account != "old-account" {
		t.Fatalf("account changed during points operation: %#v", model.Snapshot())
	}
	close(blocking.release)
	runtime.Stop()
}
