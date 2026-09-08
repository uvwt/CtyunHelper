package app

import "testing"

func TestSettingsRejectsInvalidUsagePointsWindowBeforeChangingStartup(t *testing.T) {
	paths, model, policy := newSettingsFixture(t)
	startup := &fakeStartupControl{}
	service := NewSettingsService(paths, startup, model, policy)
	if err := service.Save(GeneralSettings{
		AutomationEnabled: true, StartOnLogin: true,
		UsagePointsWindowEnabled: true, UsagePointsWindowStart: "25:00", UsagePointsWindowEnd: "06:00",
	}); err == nil {
		t.Fatal("expected invalid time error")
	}
	if startup.enabled || len(startup.calls) != 0 {
		t.Fatalf("startup changed on validation error: calls=%v enabled=%v", startup.calls, startup.enabled)
	}
}
