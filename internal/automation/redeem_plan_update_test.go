package automation

import (
	"sync"
	"testing"
	"time"
)

func TestRedeemJobUpdatePlanKeepsSafetyHistory(t *testing.T) {
	client := validRedeemClient()
	guard := NewGuard(DefaultPolicy(), SafetyState{}, GuardOptions{Now: time.Now})
	job := NewRedeemJob(client, guard, validRedeemPlan(), RedeemState{
		LastSuccessDate: "2026-09-04", LastAttemptDate: "2026-09-04", LastAttemptStatus: RedeemAttemptSuccess,
		LastRedeemTimes: 1, LastPointsSpent: 300,
	}, RedeemJobOptions{})
	updated := validRedeemPlan()
	updated.ProductID = 100
	updated.CostPoints = 700
	updated.ProductName = "季卡"
	if err := job.UpdatePlan(updated); err != nil {
		t.Fatal(err)
	}
	state := job.Snapshot()
	if state.LastSuccessDate != "2026-09-04" || state.LastPointsSpent != 300 {
		t.Fatalf("plan update reset safety history: %#v", state)
	}
	if !job.Enabled() || job.Account() != "account" {
		t.Fatalf("updated job unexpectedly disabled")
	}
}

func TestRedeemJobPlanReadsAndUpdatesAreRaceSafe(t *testing.T) {
	client := validRedeemClient()
	guard := NewGuard(DefaultPolicy(), SafetyState{}, GuardOptions{Now: time.Now})
	job := NewRedeemJob(client, guard, validRedeemPlan(), RedeemState{}, RedeemJobOptions{})
	var wait sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wait.Add(1)
		go func(offset int) {
			defer wait.Done()
			for index := 0; index < 100; index++ {
				plan := validRedeemPlan()
				plan.MaxRedeemTimes = (index + offset) % 3
				if err := job.UpdatePlan(plan); err != nil {
					t.Errorf("UpdatePlan: %v", err)
					return
				}
				_ = job.Enabled()
				_ = job.Account()
				if err := job.Validate(); err != nil {
					t.Errorf("Validate: %v", err)
					return
				}
			}
		}(worker)
	}
	wait.Wait()
}
