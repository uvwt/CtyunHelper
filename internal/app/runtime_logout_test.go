package app

import (
	"context"
	"testing"
	"time"

	"github.com/uvwt/CtyunHelper/internal/ctyun/auth"
)

func TestRuntimeLogoutStopsSessionBeforeClearingAccount(t *testing.T) {
	profile := auth.Profile{UserID: 1, SecretKey: "test", CommonLoginReqHeader: "common", BondedDevice: true}
	store := &memoryAccountStore{
		account: "account", password: "password", profile: profile, profileExists: true,
	}
	client := auth.NewClient(auth.DeviceIdentity{Code: "ctyun_fixed"}, auth.ClientOptions{})
	client.UseProfile(profile)
	model := NewModel(State{Account: "account", Connection: ConnectionOnline, Points: 680})
	flow := NewAuthFlow(client, store, model, nil)
	session := &blockingSession{}
	runtime := NewRuntime(model, flow, session, nil, RuntimeOptions{})
	runtime.Start(context.Background())

	deadline := time.Now().Add(time.Second)
	for session.starts.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if session.starts.Load() != 1 {
		t.Fatal("cloud session did not start")
	}
	if err := runtime.Logout(); err != nil {
		t.Fatal(err)
	}
	runtime.mu.Lock()
	active := runtime.sessionActive
	runtime.mu.Unlock()
	if active {
		t.Fatal("cloud session remained active after logout")
	}
	if _, ok := client.Profile(); ok {
		t.Fatal("profile remained after session stopped")
	}
	if state := model.Snapshot(); state.Account != "" || state.Connection != ConnectionAuth || state.Points != 0 {
		t.Fatalf("state after logout = %#v", state)
	}
	runtime.Stop()
}
