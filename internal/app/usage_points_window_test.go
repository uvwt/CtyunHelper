package app

import (
	"testing"
	"time"
)

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02 15:04", value, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestPointsSessionPolicySameDayWindow(t *testing.T) {
	policy, err := NewPointsSessionPolicy(UsagePointsWindow{Enabled: true, Start: "04:00", End: "07:00"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		at   string
		want bool
	}{
		{"2026-09-08 03:59", false},
		{"2026-09-08 04:00", true},
		{"2026-09-08 06:59", true},
		{"2026-09-08 07:00", false},
	}
	for _, tc := range cases {
		if got := policy.FormalAt(mustTime(t, tc.at)); got != tc.want {
			t.Fatalf("FormalAt(%s)=%v want %v", tc.at, got, tc.want)
		}
	}
	if got := policy.NextBoundary(mustTime(t, "2026-09-08 05:00")); !got.Equal(mustTime(t, "2026-09-08 07:00")) {
		t.Fatalf("active boundary = %v", got)
	}
	if got := policy.NextBoundary(mustTime(t, "2026-09-08 08:00")); !got.Equal(mustTime(t, "2026-09-09 04:00")) {
		t.Fatalf("inactive boundary = %v", got)
	}
}

func TestPointsSessionPolicyOvernightWindow(t *testing.T) {
	policy, err := NewPointsSessionPolicy(UsagePointsWindow{Enabled: true, Start: "23:30", End: "06:15"})
	if err != nil {
		t.Fatal(err)
	}
	if !policy.FormalAt(mustTime(t, "2026-09-08 23:45")) || !policy.FormalAt(mustTime(t, "2026-09-09 05:00")) {
		t.Fatal("overnight window should be formal on both sides of midnight")
	}
	if policy.FormalAt(mustTime(t, "2026-09-09 12:00")) {
		t.Fatal("overnight window should be lightweight at noon")
	}
	if got := policy.NextBoundary(mustTime(t, "2026-09-08 23:45")); !got.Equal(mustTime(t, "2026-09-09 06:15")) {
		t.Fatalf("overnight active boundary = %v", got)
	}
}

func TestPointsSessionPolicyDisabledAndUpdates(t *testing.T) {
	policy, err := NewPointsSessionPolicy(UsagePointsWindow{Start: "04:00", End: "07:00"})
	if err != nil {
		t.Fatal(err)
	}
	if policy.FormalAt(mustTime(t, "2026-09-08 05:00")) || !policy.NextBoundary(time.Now()).IsZero() {
		t.Fatal("disabled policy must stay lightweight")
	}
	if err := policy.Update(UsagePointsWindow{Enabled: true, Start: "05:00", End: "06:00"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-policy.Changes():
	default:
		t.Fatal("policy update did not signal change")
	}
	if !policy.FormalAt(mustTime(t, "2026-09-08 05:30")) {
		t.Fatal("updated policy should be formal")
	}
}

func TestNormalizeUsagePointsWindowRejectsEqualEnabledBounds(t *testing.T) {
	if _, err := normalizeUsagePointsWindow(UsagePointsWindow{Enabled: true, Start: "05:00", End: "05:00"}); err == nil {
		t.Fatal("expected equal enabled bounds to fail")
	}
	got, err := normalizeUsagePointsWindow(UsagePointsWindow{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Start != defaultUsagePointsWindowStart || got.End != defaultUsagePointsWindowEnd {
		t.Fatalf("defaults = %#v", got)
	}
}
