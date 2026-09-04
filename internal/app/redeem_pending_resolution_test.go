package app

import (
	"testing"

	"github.com/uvwt/CtyunHelper/internal/automation"
	"github.com/uvwt/CtyunHelper/internal/storage"
)

func TestRedeemSettingsPendingRequiresExplicitResolution(t *testing.T) {
	initial := storage.RedeemConfig{
		Enabled: true, Account: "account", DesktopID: 42, ProductID: 99, ProductName: "月卡",
		ProductType: "gift", CostPoints: 300, ScheduleType: automation.RedeemScheduleDaily, IntervalDays: 1,
	}
	fixture := newRedeemSettingsFixture(t, initial, automation.RedeemState{
		LastAttemptDate: "2026-09-04", LastAttemptStatus: automation.RedeemAttemptPending,
		LastRedeemTimes: 2, LastPointsSpent: 600,
	})
	view, err := fixture.service.Current()
	if err != nil {
		t.Fatal(err)
	}
	if !view.Pending || view.PendingDate != "2026-09-04" || view.PendingTimes != 2 || view.PendingPoints != 600 {
		t.Fatalf("view = %#v", view)
	}
	if fixture.model.Snapshot().RedeemEnabled {
		t.Fatal("pending result must disable scheduler/manual redeem entry")
	}
	if err := fixture.service.ResolvePending(true); err != nil {
		t.Fatal(err)
	}
	view, err = fixture.service.Current()
	if err != nil {
		t.Fatal(err)
	}
	if view.Pending {
		t.Fatalf("pending flag remained after explicit resolution: %#v", view)
	}
	if !fixture.model.Snapshot().RedeemEnabled {
		t.Fatalf("configured plan should become enabled after resolution: %#v", fixture.model.Snapshot())
	}
	if fixture.placeCalls.Load() != 0 {
		t.Fatalf("resolving pending state unexpectedly called placeOrder %d times", fixture.placeCalls.Load())
	}
}

func TestRedeemPendingResolutionRequiresConfiguredAccount(t *testing.T) {
	initial := storage.RedeemConfig{
		Enabled: true, Account: "account", DesktopID: 42, ProductID: 99, ProductName: "月卡",
		ProductType: "gift", CostPoints: 300, ScheduleType: automation.RedeemScheduleDaily, IntervalDays: 1,
	}
	fixture := newRedeemSettingsFixture(t, initial, automation.RedeemState{
		LastAttemptDate: "2026-09-04", LastAttemptStatus: automation.RedeemAttemptPending,
	})
	fixture.model.Update(func(state *State) { state.Account = "other-account" })
	if err := fixture.service.ResolvePending(false); err == nil {
		t.Fatal("cross-account pending resolution should be rejected")
	}
	if fixture.job.Snapshot().LastAttemptStatus != automation.RedeemAttemptPending {
		t.Fatalf("pending state changed across accounts: %#v", fixture.job.Snapshot())
	}
}
