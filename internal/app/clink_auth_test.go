package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uvwt/CtyunHelper/internal/automation"
	"github.com/uvwt/CtyunHelper/internal/ctyun/auth"
)

type clinkAuthTestStore struct {
	account  string
	password string
}

func (s *clinkAuthTestStore) LoadLogin() (string, string, error) {
	return s.account, s.password, nil
}

func (*clinkAuthTestStore) SaveClinkProfile(string, auth.Profile) error { return nil }

func (*clinkAuthTestStore) LoadClinkProfile(string) (auth.Profile, error) {
	return auth.Profile{}, errors.New("profile not found")
}

func (*clinkAuthTestStore) DeleteClinkProfile() error { return nil }

func TestClinkAuthUsesIndependentQuotaAndStopsOnManualLoginRequirement(t *testing.T) {
	var loginCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/client/genChallengeData":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"challengeId": "challenge", "challengeCode": "salt"},
			})
		case "/api/auth/client/login":
			loginCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": auth.CodeNeedCaptcha,
				"msg":  "用户名或密码错误",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := auth.NewClient(
		auth.DeviceIdentity{Code: "device-code"},
		auth.ClientOptions{APIOrigin: server.URL, HTTPClient: server.Client()},
	)
	guard := automation.NewGuard(automation.DefaultPolicy(), automation.SafetyState{}, automation.GuardOptions{
		Now: func() time.Time { return time.Date(2026, 10, 15, 12, 0, 0, 0, time.Local) },
	})
	flow := newClinkAuthFlow(
		client,
		&clinkAuthTestStore{account: "account", password: "password"},
		guard,
	)

	_, err := flow.refresh(context.Background(), "account")
	if !errors.Is(err, errClinkLoginActionRequired) {
		t.Fatalf("refresh() error = %v, want manual-action marker", err)
	}
	if loginCalls.Load() != 1 {
		t.Fatalf("login calls = %d, want 1", loginCalls.Load())
	}
	state := guard.Snapshot()
	if state.DailyActions[automation.ActionClinkLogin] != 1 || state.DailyActions[automation.ActionLogin] != 0 {
		t.Fatalf("login quotas were not isolated: %#v", state.DailyActions)
	}
	if state.ConsecutiveFailures != 0 || !state.BlockedUntil.IsZero() {
		t.Fatalf("manual login requirement must not enter failure cooldown: %#v", state)
	}
}
