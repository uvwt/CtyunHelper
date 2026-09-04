package automation

import (
	"errors"
	"testing"
	"time"
)

func TestResolvePendingPersistenceFailureKeepsMemoryPending(t *testing.T) {
	client := validRedeemClient()
	guard := NewGuard(DefaultPolicy(), SafetyState{}, GuardOptions{Now: time.Now})
	job := NewRedeemJob(client, guard, validRedeemPlan(), RedeemState{
		LastAttemptDate: "2026-09-04", LastAttemptStatus: RedeemAttemptPending,
		LastRedeemTimes: 2, LastPointsSpent: 600,
	}, RedeemJobOptions{SaveState: func(RedeemState) error { return errors.New("disk failed") }})
	if err := job.ResolvePending(true); err == nil {
		t.Fatal("expected persistence failure")
	}
	state := job.Snapshot()
	if state.LastAttemptStatus != RedeemAttemptPending || state.LastSuccessDate != "" {
		t.Fatalf("memory state changed despite persistence failure: %#v", state)
	}
}
