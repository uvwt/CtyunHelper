package automation

import (
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
