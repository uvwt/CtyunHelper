package automation

import (
	"errors"
	"testing"
	"time"
)

func TestDefaultPolicyAllowsTwoAIAttempts(t *testing.T) {
	policy := DefaultPolicy()
	var state SafetyState
	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.Local)
	if err := state.Claim(now, policy, ActionAI); err != nil {
		t.Fatal(err)
	}
	if err := state.Claim(now, policy, ActionAI); err != nil {
		t.Fatal(err)
	}
	if err := state.Claim(now, policy, ActionAI); err == nil {
		t.Fatal("third AI attempt should be blocked")
	}
}

func TestLoginAndClinkLoginHaveIndependentDailyQuota(t *testing.T) {
	policy := DefaultPolicy()
	var state SafetyState
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.Local)

	for i := 0; i < policy.MaxLogin; i++ {
		if err := state.Claim(now, policy, ActionLogin); err != nil {
			t.Fatalf("primary login claim %d: %v", i+1, err)
		}
	}
	for i := 0; i < policy.MaxLogin; i++ {
		if err := state.Claim(now, policy, ActionClinkLogin); err != nil {
			t.Fatalf("Clink login claim %d: %v", i+1, err)
		}
	}
	if err := state.Claim(now, policy, ActionLogin); err == nil {
		t.Fatal("primary login should be blocked after its own quota is exhausted")
	}
	if err := state.Claim(now, policy, ActionClinkLogin); err == nil {
		t.Fatal("Clink login should be blocked after its own quota is exhausted")
	}
}

func TestFailuresEnterCooldown(t *testing.T) {
	policy := DefaultPolicy()
	var state SafetyState
	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.Local)
	for i := 0; i < policy.MaxFailures; i++ {
		state.RecordFailure(now, policy)
	}
	if !state.BlockedUntil.Equal(now.Add(6 * time.Hour)) {
		t.Fatalf("blockedUntil = %s", state.BlockedUntil)
	}
	if err := state.Claim(now.Add(time.Hour), policy, ActionLogin); err == nil {
		t.Fatal("expected cooldown to block login")
	}
}

func TestExpiredCooldownResetsConsecutiveFailures(t *testing.T) {
	policy := DefaultPolicy()
	started := time.Date(2026, 9, 4, 10, 0, 0, 0, time.Local)
	state := SafetyState{ConsecutiveFailures: policy.MaxFailures, BlockedUntil: started.Add(policy.FailureCooldown)}
	afterCooldown := state.BlockedUntil.Add(time.Minute)
	if err := state.Claim(afterCooldown, policy, ActionAI); err != nil {
		t.Fatal(err)
	}
	if state.ConsecutiveFailures != 0 || !state.BlockedUntil.IsZero() {
		t.Fatalf("expired cooldown was not reset: %#v", state)
	}
	state.RecordFailure(afterCooldown, policy)
	if state.ConsecutiveFailures != 1 || !state.BlockedUntil.IsZero() {
		t.Fatalf("first failure after cooldown should not immediately re-block: %#v", state)
	}
}

func TestGuardPersistsQuotaBeforeReturningClaim(t *testing.T) {
	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.Local)
	var saved SafetyState
	guard := NewGuard(DefaultPolicy(), SafetyState{}, GuardOptions{
		Now: func() time.Time { return now },
		Save: func(state SafetyState) error {
			saved = state
			return nil
		},
	})
	if err := guard.Claim(ActionAI); err != nil {
		t.Fatal(err)
	}
	if saved.Date != "2026-09-04" || saved.DailyActions[ActionAI] != 1 {
		t.Fatalf("saved = %#v", saved)
	}
}

func TestGuardRollsBackQuotaWhenPersistenceFails(t *testing.T) {
	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.Local)
	guard := NewGuard(DefaultPolicy(), SafetyState{}, GuardOptions{
		Now:  func() time.Time { return now },
		Save: func(SafetyState) error { return errors.New("disk failed") },
	})
	if err := guard.Claim(ActionAI); err == nil {
		t.Fatal("expected persistence error")
	}
	state := guard.Snapshot()
	if state.Date != "" || len(state.DailyActions) != 0 {
		t.Fatalf("quota must roll back after persistence failure: %#v", state)
	}
}
