package clink

import "testing"

func TestHappyPathTransitions(t *testing.T) {
	path := []State{StateIdle, StateResolving, StateConnecting, StateHandshaking, StateOnline, StateStopped}
	for i := 1; i < len(path); i++ {
		if err := ValidateTransition(path[i-1], path[i]); err != nil {
			t.Fatalf("transition %s -> %s: %v", path[i-1], path[i], err)
		}
	}
}

func TestRejectsImpossibleTransition(t *testing.T) {
	if err := ValidateTransition(StateIdle, StateOnline); err == nil {
		t.Fatal("expected idle -> online to be rejected")
	}
}
