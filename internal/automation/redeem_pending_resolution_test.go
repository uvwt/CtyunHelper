package automation

import (
	"testing"
	"time"
)

func TestResolvePendingSuccessKeepsSameDayBlocked(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 9, 4, 19, 0, 0, 0, location)
	client := validRedeemClient()
	guard := NewGuard(DefaultPolicy(), SafetyState{}, GuardOptions{Now: func() time.Time { return now }})
	var saved RedeemState
	job := NewRedeemJob(client, guard, validRedeemPlan(), RedeemState{
		LastAttemptDate: "2026-09-04", LastAttemptStatus: RedeemAttemptPending,
		LastRedeemTimes: 2, LastPointsSpent: 600,
	}, RedeemJobOptions{Now: func() time.Time { return now }, SaveState: func(state RedeemState) error {
		saved = state
		return nil
	}})
	if err := job.ResolvePending(true); err != nil {
		t.Fatal(err)
	}
	state := job.Snapshot()
	if state.LastAttemptStatus != RedeemAttemptSuccess || state.LastSuccessDate != "2026-09-04" || state.LastRedeemTimes != 2 || state.LastPointsSpent != 600 {
		t.Fatalf("state = %#v", state)
	}
	if saved.LastAttemptStatus != RedeemAttemptSuccess {
		t.Fatalf("saved = %#v", saved)
	}
	allowed, _, err := ShouldRedeemToday(validRedeemPlan(), state, now)
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("confirmed success must remain blocked for the same day")
	}
}

func TestResolvePendingFailureAlsoKeepsSameDayBlocked(t *testing.T) {
	now := time.Date(2026, 9, 4, 19, 0, 0, 0, time.Local)
	client := validRedeemClient()
	guard := NewGuard(DefaultPolicy(), SafetyState{}, GuardOptions{Now: func() time.Time { return now }})
	job := NewRedeemJob(client, guard, validRedeemPlan(), RedeemState{
		LastAttemptDate: "2026-09-04", LastAttemptStatus: RedeemAttemptPending,
		LastRedeemTimes: 2, LastPointsSpent: 600,
	}, RedeemJobOptions{Now: func() time.Time { return now }})
	if err := job.ResolvePending(false); err != nil {
		t.Fatal(err)
	}
	state := job.Snapshot()
	if state.LastAttemptStatus != RedeemAttemptFailed || state.LastSuccessDate != "" {
		t.Fatalf("state = %#v", state)
	}
	allowed, _, err := ShouldRedeemToday(validRedeemPlan(), state, now)
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("confirmed failure must not retry on the same day")
	}
}
