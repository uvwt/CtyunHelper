package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uvwt/CtyunHelper/internal/automation"
	"github.com/uvwt/CtyunHelper/internal/ctyun/auth"
	"github.com/uvwt/CtyunHelper/internal/ctyun/points"
)

func TestRecoveringPointsClientRecovers40010AndRetriesReadOnlyRequest(t *testing.T) {
	var taskCalls atomic.Int32
	var loginCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/selforder/api/marketing/userPoints/getTaskList":
			if taskCalls.Add(1) == 1 {
				_ = json.NewEncoder(w).Encode(map[string]any{"code": auth.CodeNoPermissions, "msg": "当前登录信息已过期，请重新登录"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": []map[string]any{{
				"taskDefName": automation.UsageTaskName, "status": automation.TaskDone, "currentProgress": 3600,
			}}})
		case "/api/auth/client/genChallengeData":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"challengeId": "challenge", "challengeCode": "salt"}})
		case "/api/auth/client/login":
			loginCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
				"userId": 2, "userEid": "new-eid", "tenantId": 3,
				"secretKey": "new-key", "commonLoginReqHeader": "new-common", "bondedDevice": true,
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldProfile := auth.Profile{
		UserID: 1, UserEID: "old-eid", TenantID: 2,
		SecretKey: "old-key", CommonLoginReqHeader: "old-common", BondedDevice: true,
	}
	store := &memoryAccountStore{account: "account", password: "password", profile: oldProfile, profileExists: true}
	authClient := auth.NewClient(auth.DeviceIdentity{Code: "ctyun_fixed"}, auth.ClientOptions{
		APIOrigin: server.URL, HTTPClient: server.Client(), Random: strings.NewReader(strings.Repeat("x", 8192)),
	})
	model := NewModel(State{})
	guard := automation.NewGuard(automation.DefaultPolicy(), automation.SafetyState{}, automation.GuardOptions{Now: time.Now})
	flow := NewAuthFlow(authClient, store, model, guard)
	if restored, err := flow.Restore("account"); err != nil || !restored {
		t.Fatalf("Restore() restored=%v err=%v", restored, err)
	}
	model.Update(func(state *State) { state.Connection = ConnectionOnline })

	client := NewRecoveringPointsClient(points.NewClient(authClient, points.ClientOptions{Origin: server.URL}), flow)
	tasks, err := client.Tasks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Name != automation.UsageTaskName || tasks[0].Status != automation.TaskDone {
		t.Fatalf("tasks = %#v", tasks)
	}
	if taskCalls.Load() != 2 || loginCalls.Load() != 1 {
		t.Fatalf("task calls=%d login calls=%d", taskCalls.Load(), loginCalls.Load())
	}
	if state := model.Snapshot(); state.Connection != ConnectionOnline || state.LastError != "" {
		t.Fatalf("state after recovery = %#v", state)
	}
	if safety := guard.Snapshot(); safety.DailyActions[automation.ActionLogin] != 1 || safety.ConsecutiveFailures != 0 {
		t.Fatalf("safety after recovery = %#v", safety)
	}
}

func TestRecoveringPointsClientDoesNotRetryPlaceOrder(t *testing.T) {
	var placeCalls atomic.Int32
	var loginCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/selforder/api/selforder/paas/placeOrder":
			placeCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": auth.CodeNoPermissions, "msg": "当前登录信息已过期，请重新登录"})
		case "/api/auth/client/login":
			loginCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	profile := auth.Profile{UserID: 1, UserEID: "eid", TenantID: 2, SecretKey: "key", CommonLoginReqHeader: "common", BondedDevice: true}
	store := &memoryAccountStore{account: "account", password: "password", profile: profile, profileExists: true}
	authClient := auth.NewClient(auth.DeviceIdentity{Code: "ctyun_fixed"}, auth.ClientOptions{
		APIOrigin: server.URL, HTTPClient: server.Client(), Random: strings.NewReader(strings.Repeat("y", 8192)),
	})
	model := NewModel(State{})
	flow := NewAuthFlow(authClient, store, model, nil)
	if restored, err := flow.Restore("account"); err != nil || !restored {
		t.Fatalf("Restore() restored=%v err=%v", restored, err)
	}

	client := NewRecoveringPointsClient(points.NewClient(authClient, points.ClientOptions{Origin: server.URL}), flow)
	_, err := client.PlaceOrder(context.Background(), points.OrderRequest{BusinessChannel: "010", OrderType: 1})
	if err == nil || !auth.RequiresAuthentication(err) {
		t.Fatalf("PlaceOrder() error = %v, want classified 40010", err)
	}
	if placeCalls.Load() != 1 || loginCalls.Load() != 0 {
		t.Fatalf("place calls=%d login calls=%d", placeCalls.Load(), loginCalls.Load())
	}
}
