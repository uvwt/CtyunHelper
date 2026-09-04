package app

import (
	"errors"
	"testing"

	"github.com/uvwt/CtyunHelper/internal/automation"
	"github.com/uvwt/CtyunHelper/internal/ctyun/auth"
	"github.com/uvwt/CtyunHelper/internal/storage"
)

type failingLogoutStore struct {
	*memoryAccountStore
}

func (s *failingLogoutStore) DeleteLogin() error {
	return errors.New("credential delete failed")
}

func TestRuntimeLogoutDisablesRedeemEvenWhenLocalCleanupReportsError(t *testing.T) {
	initial := storage.RedeemConfig{
		Enabled: true, Account: "account", DesktopID: 42, ProductID: 99, ProductName: "月卡",
		ProductType: "gift", CostPoints: 300, ScheduleType: automation.RedeemScheduleDaily, IntervalDays: 1,
	}
	fixture := newRedeemSettingsFixture(t, initial, automation.RedeemState{})
	if !fixture.model.Snapshot().RedeemEnabled {
		t.Fatal("fixture redeem plan should start enabled")
	}
	profile := auth.Profile{UserID: 1, SecretKey: "test", CommonLoginReqHeader: "common", BondedDevice: true}
	store := &failingLogoutStore{memoryAccountStore: &memoryAccountStore{
		account: "account", password: "password", profile: profile, profileExists: true,
	}}
	client := auth.NewClient(auth.DeviceIdentity{Code: "ctyun_fixed"}, auth.ClientOptions{})
	client.UseProfile(profile)
	flow := NewAuthFlow(client, store, fixture.model, nil)
	runtime := NewRuntime(fixture.model, flow, nil, fixture.tasks, RuntimeOptions{})
	if err := runtime.Logout(); err == nil {
		t.Fatal("expected credential cleanup error")
	}
	state := fixture.model.Snapshot()
	if state.Account != "" || state.RedeemEnabled {
		t.Fatalf("logout error left account-scoped actions enabled: %#v", state)
	}
}
