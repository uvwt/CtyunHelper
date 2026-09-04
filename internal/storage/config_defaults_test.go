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
