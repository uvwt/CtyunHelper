package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/uvwt/CtyunHelper/internal/ctyun/auth"
)

func TestFailedCandidateLoginPreservesCurrentProfileAndModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/client/genChallengeData":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"challengeId": "challenge", "challengeCode": "salt"}})
		case "/api/auth/client/login":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 51010, "msg": "password invalid"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldProfile := auth.Profile{
		UserID: 1, UserEID: "old-eid", TenantID: 2,
		SecretKey: "old-key", CommonLoginReqHeader: "old-common", BondedDevice: true,
	}
	client := auth.NewClient(auth.DeviceIdentity{Code: "ctyun_fixed"}, auth.ClientOptions{
		APIOrigin: server.URL, HTTPClient: server.Client(),
	})
	client.UseProfile(oldProfile)
	model := NewModel(State{Account: "old-account", Connection: ConnectionOnline})
	flow := NewAuthFlow(client, &memoryAccountStore{}, model, nil)

	if _, err := flow.CompleteLogin(context.Background(), "new-account", "wrong-password", "1234", "captcha-key"); err == nil {
		t.Fatal("expected candidate login failure")
	}
	profile, ok := client.Profile()
	if !ok || profile.UserID != oldProfile.UserID || profile.SecretKey != oldProfile.SecretKey {
		t.Fatalf("current profile changed: %#v, ok=%v", profile, ok)
	}
	state := model.Snapshot()
	if state.Account != "old-account" || state.Connection != ConnectionOnline {
		t.Fatalf("current model changed: %#v", state)
	}
}

func TestGenericBindingErrorPreservesBindingState(t *testing.T) {
	client := auth.NewClient(auth.DeviceIdentity{Code: "ctyun_fixed"}, auth.ClientOptions{})
	client.UseProfile(auth.Profile{UserID: 1, SecretKey: "key", CommonLoginReqHeader: "common", BondedDevice: false})
	model := NewModel(State{Account: "account", Connection: ConnectionDeviceBind})
	flow := NewAuthFlow(client, &memoryAccountStore{}, model, nil)

	flow.setAuthError(errors.New("network timeout"))
	state := model.Snapshot()
	if state.Connection != ConnectionDeviceBind || state.LastError != "network timeout" {
		t.Fatalf("generic error changed binding state: %#v", state)
	}

	flow.setAuthError(auth.APIError{Code: auth.CodeNoPermissions, Message: "expired"})
	if got := model.Snapshot().Connection; got != ConnectionAuth {
		t.Fatalf("explicit auth failure state = %s", got)
	}
}
