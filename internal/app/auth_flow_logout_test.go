package app

import (
	"testing"
	"time"

	"github.com/uvwt/CtyunHelper/internal/ctyun/auth"
)

func TestAuthFlowLogoutClearsPersistedAccountAndVisibleAccountState(t *testing.T) {
	profile := auth.Profile{UserID: 123, SecretKey: "test", CommonLoginReqHeader: "common", BondedDevice: true}
	store := &memoryAccountStore{
		account: "account", password: "password", profile: profile, profileExists: true, clinkProfileExists: true,
	}
	client := auth.NewClient(auth.DeviceIdentity{Code: "ctyun_fixed"}, auth.ClientOptions{})
	client.UseProfile(profile)
	model := NewModel(State{
		Account: "account", Connection: ConnectionOnline,
		DesktopID: "desktop", DesktopName: "测试云电脑", OnlineSince: time.Now(),
		Points: 680, UsageTask: UsageTaskStatus{Found: true, Status: 2, Progress: 1},
	})
	flow := NewAuthFlow(client, store, model, nil)
	if err := flow.Logout(); err != nil {
		t.Fatal(err)
	}
	if _, ok := client.Profile(); ok {
		t.Fatal("client profile remained after logout")
	}
	if store.account != "" || store.password != "" || store.profileExists || store.clinkProfileExists {
		t.Fatalf("persisted login remained: account=%q password=%q profile=%v clinkProfile=%v", store.account, store.password, store.profileExists, store.clinkProfileExists)
	}
	if store.deleteClinkProfile != 1 {
		t.Fatalf("Clink profile delete count = %d, want 1", store.deleteClinkProfile)
	}
	state := model.Snapshot()
	if state.Account != "" || state.Connection != ConnectionAuth || state.DesktopID != "" || state.DesktopName != "" || !state.OnlineSince.IsZero() || state.Points != 0 || state.UsageTask.Found {
		t.Fatalf("visible account state remained after logout: %#v", state)
	}
}
