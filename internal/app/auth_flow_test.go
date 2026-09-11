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

func TestAuthFlowRecoversExpiredProfileWithStoredLoginOnce(t *testing.T) {
	var loginCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
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

	oldProfile := auth.Profile{UserID: 1, UserEID: "old-eid", TenantID: 2, SecretKey: "old-key", CommonLoginReqHeader: "old-common", BondedDevice: true}
	store := &memoryAccountStore{account: "account", password: "password", profile: oldProfile, profileExists: true}
	client := auth.NewClient(auth.DeviceIdentity{Code: "ctyun_fixed"}, auth.ClientOptions{APIOrigin: server.URL, HTTPClient: server.Client()})
	model := NewModel(State{})
	guard := automation.NewGuard(automation.DefaultPolicy(), automation.SafetyState{}, automation.GuardOptions{Now: time.Now})
	flow := NewAuthFlow(client, store, model, guard)
	if restored, err := flow.Restore("account"); err != nil || !restored {
		t.Fatalf("Restore() restored=%v err=%v", restored, err)
	}
	model.Update(func(state *State) { state.Connection = ConnectionOnline })
	failedRevision := flow.currentProfileRevision()

	if err := flow.RecoverExpiredProfile(context.Background(), failedRevision); err != nil {
		t.Fatal(err)
	}
	profile, ok := client.Profile()
	if !ok || profile.UserID != 2 || store.profile.UserID != 2 || !store.profileExists {
		t.Fatalf("recovered profile client=%#v ok=%v store=%#v exists=%v", profile, ok, store.profile, store.profileExists)
	}
	if state := model.Snapshot(); state.Connection != ConnectionOnline || state.LastError != "" {
		t.Fatalf("recovery changed healthy connection state: %#v", state)
	}
	if loginCalls.Load() != 1 || guard.Snapshot().DailyActions[automation.ActionLogin] != 1 {
		t.Fatalf("login calls=%d safety=%#v", loginCalls.Load(), guard.Snapshot())
	}

	// 同一批并发失败携带旧 revision 时，第二个恢复请求只能复用新 Profile。
	if err := flow.RecoverExpiredProfile(context.Background(), failedRevision); err != nil {
		t.Fatal(err)
	}
	if loginCalls.Load() != 1 {
		t.Fatalf("deduplicated recovery login calls = %d, want 1", loginCalls.Load())
	}
}

func TestAuthFlowExpiredProfileCaptchaFallsBackToInteractiveLogin(t *testing.T) {
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

	oldProfile := auth.Profile{UserID: 1, UserEID: "old-eid", TenantID: 2, SecretKey: "old-key", CommonLoginReqHeader: "old-common", BondedDevice: true}
	store := &memoryAccountStore{account: "account", password: "password", profile: oldProfile, profileExists: true}
	client := auth.NewClient(auth.DeviceIdentity{Code: "ctyun_fixed"}, auth.ClientOptions{APIOrigin: server.URL, HTTPClient: server.Client()})
	model := NewModel(State{})
	guard := automation.NewGuard(automation.DefaultPolicy(), automation.SafetyState{}, automation.GuardOptions{Now: time.Now})
	flow := NewAuthFlow(client, store, model, guard)
	if restored, err := flow.Restore("account"); err != nil || !restored {
		t.Fatalf("Restore() restored=%v err=%v", restored, err)
	}
	model.Update(func(state *State) { state.Connection = ConnectionOnline })

	err := flow.RecoverExpiredProfile(context.Background(), flow.currentProfileRevision())
	if err == nil || !auth.RequiresLoginCaptcha(err) {
		t.Fatalf("RecoverExpiredProfile() error = %v, want captcha classification", err)
	}
	if _, ok := client.Profile(); ok || store.profileExists {
		t.Fatal("expired profile remained after captcha fallback")
	}
	state := model.Snapshot()
	if state.Connection != ConnectionAuth || state.LastError == "" {
		t.Fatalf("captcha fallback state = %#v", state)
	}
	if safety := guard.Snapshot(); safety.DailyActions[automation.ActionLogin] != 1 || safety.ConsecutiveFailures != 0 {
		t.Fatalf("captcha fallback safety = %#v", safety)
	}

	// 用户随后打开登录窗口时，第一次无验证码提交只是把同一流程推进到验证码界面，
	// 不应因为自动恢复已经占用过一次额度而再次 Claim。
	_, err = flow.CompleteLogin(context.Background(), "account", "password", "", "")
	if !auth.RequiresLoginCaptcha(err) {
		t.Fatalf("interactive continuation error = %v, want captcha", err)
	}
	if safety := guard.Snapshot(); safety.DailyActions[automation.ActionLogin] != 1 {
		t.Fatalf("interactive continuation consumed another login quota: %#v", safety)
	}
}

func TestAuthFlowRecoveredUnboundProfileRequiresDeviceBinding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/client/genChallengeData":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"challengeId": "challenge", "challengeCode": "salt"}})
		case "/api/auth/client/login":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
				"userId": 2, "userEid": "new-eid", "tenantId": 3,
				"secretKey": "new-key", "commonLoginReqHeader": "new-common", "bondedDevice": false,
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldProfile := auth.Profile{UserID: 1, UserEID: "old-eid", TenantID: 2, SecretKey: "old-key", CommonLoginReqHeader: "old-common", BondedDevice: true}
	store := &memoryAccountStore{account: "account", password: "password", profile: oldProfile, profileExists: true}
	client := auth.NewClient(auth.DeviceIdentity{Code: "ctyun_fixed"}, auth.ClientOptions{APIOrigin: server.URL, HTTPClient: server.Client()})
	model := NewModel(State{})
	flow := NewAuthFlow(client, store, model, nil)
	if restored, err := flow.Restore("account"); err != nil || !restored {
		t.Fatalf("Restore() restored=%v err=%v", restored, err)
	}

	if err := flow.RecoverExpiredProfile(context.Background(), flow.currentProfileRevision()); err == nil {
		t.Fatal("unbound recovery should require user action")
	}
	profile, ok := client.Profile()
	if !ok || profile.BondedDevice || !store.profileExists {
		t.Fatalf("unbound recovered profile client=%#v ok=%v storeExists=%v", profile, ok, store.profileExists)
	}
	if state := model.Snapshot(); state.Connection != ConnectionDeviceBind || state.LastError == "" {
		t.Fatalf("unbound recovery state = %#v", state)
	}
}

func TestAuthFlowDoesNotRecoverExpiredProfileWithAnotherAccountsCredential(t *testing.T) {
	profile := auth.Profile{UserID: 1, UserEID: "eid", TenantID: 2, SecretKey: "key", CommonLoginReqHeader: "common", BondedDevice: true}
	store := &memoryAccountStore{account: "other-account", password: "password", profile: profile, profileExists: true}
	client := auth.NewClient(auth.DeviceIdentity{Code: "ctyun_fixed"}, auth.ClientOptions{})
	model := NewModel(State{})
	flow := NewAuthFlow(client, store, model, nil)
	if restored, err := flow.Restore("current-account"); err != nil || !restored {
		t.Fatalf("Restore() restored=%v err=%v", restored, err)
	}

	if err := flow.RecoverExpiredProfile(context.Background(), flow.currentProfileRevision()); err == nil {
		t.Fatal("mismatched stored credential must not be used for recovery")
	}
	if _, ok := client.Profile(); ok || store.profileExists {
		t.Fatal("expired profile remained after credential mismatch")
	}
	if state := model.Snapshot(); state.Connection != ConnectionAuth || state.LastError == "" {
		t.Fatalf("credential mismatch state = %#v", state)
	}
}
