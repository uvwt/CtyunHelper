package storage

import "testing"

func TestDefaultConfigKeepsRedeemDisabled(t *testing.T) {
	config := DefaultConfig()
	if config.Redeem.Enabled {
		t.Fatal("automatic redeem must be opt-in")
	}
	if config.Redeem.ScheduleType != "daily" || config.Redeem.IntervalDays != 1 {
		t.Fatalf("redeem defaults = %#v", config.Redeem)
	}
}

func TestDefaultConfigKeepsUsagePointsWindowDisabled(t *testing.T) {
	config := DefaultConfig()
	window := config.Automation.UsagePointsWindow
	if window.Enabled {
		t.Fatal("formal usage-points session must be opt-in")
	}
	if window.Start != "04:00" || window.End != "07:00" {
		t.Fatalf("usage-points window defaults = %#v", window)
	}
}
