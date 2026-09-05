package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uvwt/CtyunHelper/internal/automation"
	"github.com/uvwt/CtyunHelper/internal/ctyun/auth"
)

type memoryAccountStore struct {
	account            string
	password           string
	profile            auth.Profile
	profileExists      bool
	deleteProfile      int
	clinkProfileExists bool
	deleteClinkProfile int
}

func (s *memoryAccountStore) SaveAccount(account string) error {
	s.account = account
	return nil
}
func (s *memoryAccountStore) SaveLogin(account, password string) error {
	s.account, s.password = account, password
	return nil
}
func (s *memoryAccountStore) LoadLogin() (string, string, error) {
	if s.account == "" {
		return "", "", os.ErrNotExist
	}
	return s.account, s.password, nil
}
func (s *memoryAccountStore) DeleteLogin() error {
	s.account, s.password = "", ""
	return nil
}
func (s *memoryAccountStore) SaveProfile(_ string, profile auth.Profile) error {
	s.profile, s.profileExists = profile, true
	return nil
}
func (s *memoryAccountStore) LoadProfile(string) (auth.Profile, error) {
	if !s.profileExists {
		return auth.Profile{}, os.ErrNotExist
	}
	return s.profile, nil
}
func (s *memoryAccountStore) DeleteProfile() error {
	s.profileExists = false
	s.deleteProfile++
	return nil
}
func (s *memoryAccountStore) DeleteClinkProfile() error {
	s.clinkProfileExists = false
	s.deleteClinkProfile++
	return nil
}

func TestAuthFlowRestoresCachedProfileWithoutRelogin(t *testing.T) {
	profile := auth.Profile{UserID: 123, SecretKey: "test", CommonLoginReqHeader: "common", BondedDevice: true}
	store := &memoryAccountStore{profile: profile, profileExists: true}
	client := auth.NewClient(auth.DeviceIdentity{Code: "ctyun_fixed"}, auth.ClientOptions{})
	model := NewModel(State{})
	flow := NewAuthFlow(client, store, model, nil)
	restored, err := flow.Restore("account")
	if err != nil || !restored {
		t.Fatalf("Restore() restored=%v err=%v", restored, err)
	}
	got, ok := client.Profile()
	if !ok || got.UserID != 123 {
		t.Fatalf("client profile = %#v, ok=%v", got, ok)
	}
	if model.Snapshot().Connection != ConnectionStopped {
		t.Fatalf("state = %#v", model.Snapshot())
	}
}

func TestAuthFlowOnlyClearsProfileOnExplicitAuthFailure(t *testing.T) {
	profile := auth.Profile{UserID: 123, SecretKey: "test", CommonLoginReqHeader: "common", BondedDevice: true}
	store := &memoryAccountStore{profile: profile, profileExists: true}
	client := auth.NewClient(auth.DeviceIdentity{Code: "ctyun_fixed"}, auth.ClientOptions{})
	client.UseProfile(profile)
	model := NewModel(State{Account: "account"})
	flow := NewAuthFlow(client, store, model, nil)

	if err := flow.HandleSessionError(errors.New("network down")); err != nil {
		t.Fatal(err)
	}
	if _, ok := client.Profile(); !ok || store.deleteProfile != 0 {
		t.Fatal("network error must not clear profile")
	}

	if err := flow.HandleSessionError(auth.APIError{Code: auth.CodeNoPermissions, Message: "expired"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := client.Profile(); ok || store.profileExists || store.deleteProfile != 1 {
		t.Fatal("auth failure must clear cached profile")
	}
	if model.Snapshot().Connection != ConnectionAuth {
		t.Fatalf("state = %#v", model.Snapshot())
	}
}

func TestAuthFlowKeepsUnboundProfileForBinding(t *testing.T) {
	profile := auth.Profile{UserID: 123, SecretKey: "test", CommonLoginReqHeader: "common", BondedDevice: false}
	store := &memoryAccountStore{profile: profile, profileExists: true}
	client := auth.NewClient(auth.DeviceIdentity{Code: "ctyun_fixed"}, auth.ClientOptions{})
	model := NewModel(State{})
	flow := NewAuthFlow(client, store, model, nil)
	if _, err := flow.Restore("account"); err != nil {
		t.Fatal(err)
	}
	if model.Snapshot().Connection != ConnectionDeviceBind {
		t.Fatalf("state = %#v", model.Snapshot())
	}
	if _, ok := client.Profile(); !ok {
		t.Fatal("unbound profile must remain available for binding")
	}
}

func TestAuthFlowLimitsRealLoginRequestsToTwoPerDay(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/client/genChallengeData":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"challengeId": "challenge", "challengeCode": "salt"}})
		case "/api/auth/client/login":
			calls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 50001, "msg": "login failed"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := auth.NewClient(auth.DeviceIdentity{Code: "ctyun_fixed"}, auth.ClientOptions{
		APIOrigin: server.URL, HTTPClient: server.Client(),
	})
	model := NewModel(State{})
	guard := automation.NewGuard(automation.DefaultPolicy(), automation.SafetyState{}, automation.GuardOptions{
		Now: func() time.Time { return time.Date(2026, 9, 4, 10, 0, 0, 0, time.Local) },
	})
	flow := NewAuthFlow(client, &memoryAccountStore{}, model, guard)
	for i := 0; i < 2; i++ {
		if _, err := flow.CompleteLogin(context.Background(), "account", "password", "", ""); err == nil {
			t.Fatal("expected login failure")
		}
	}
	if _, err := flow.CompleteLogin(context.Background(), "account", "password", "", ""); err == nil {
		t.Fatal("third login should be blocked by daily policy")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("real login calls = %d, want 2", got)
	}
	if got := guard.Snapshot().DailyActions[automation.ActionLogin]; got != 2 {
		t.Fatalf("login quota = %d, want 2", got)
	}
}

func TestAuthFlowCaptchaContinuationReusesInitialLoginQuota(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/client/genChallengeData":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"challengeId": "challenge", "challengeCode": "salt"}})
		case "/api/auth/client/login":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": auth.CodeNeedCaptcha, "msg": "captcha required"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := auth.NewClient(auth.DeviceIdentity{Code: "ctyun_fixed"}, auth.ClientOptions{APIOrigin: server.URL, HTTPClient: server.Client()})
	guard := automation.NewGuard(automation.DefaultPolicy(), automation.SafetyState{}, automation.GuardOptions{
		Now: func() time.Time { return time.Date(2026, 9, 4, 10, 0, 0, 0, time.Local) },
	})
	flow := NewAuthFlow(client, &memoryAccountStore{}, NewModel(State{}), guard)
	_, err := flow.CompleteLogin(context.Background(), "account", "password", "", "")
	if !auth.RequiresLoginCaptcha(err) {
		t.Fatalf("expected captcha continuation, got %v", err)
	}
	state := guard.Snapshot()
	if state.DailyActions[automation.ActionLogin] != 1 || state.ConsecutiveFailures != 0 {
		t.Fatalf("initial login should consume one quota without counting a failure: %#v", state)
	}

	_, err = flow.CompleteLogin(context.Background(), "account", "password", "1234", "captcha-key")
	if !auth.RequiresLoginCaptcha(err) {
		t.Fatalf("expected captcha refresh continuation, got %v", err)
	}
	state = guard.Snapshot()
	if state.DailyActions[automation.ActionLogin] != 1 || state.ConsecutiveFailures != 0 {
		t.Fatalf("captcha continuation must reuse initial login quota: %#v", state)
	}
}
