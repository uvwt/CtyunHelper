package automation

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRedeemInsufficientPointsDoesNotConsumeQuota(t *testing.T) {
	client := validRedeemClient()
	client.points = 299
	guard := NewGuard(DefaultPolicy(), SafetyState{}, GuardOptions{Now: time.Now})
	job := NewRedeemJob(client, guard, validRedeemPlan(), RedeemState{}, RedeemJobOptions{})
	result, err := job.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.SkippedReason == "" || client.placeCalls != 0 || guard.Snapshot().DailyActions[ActionRedeem] != 0 {
		t.Fatalf("result=%#v calls=%d safety=%#v", result, client.placeCalls, guard.Snapshot())
	}
}

func TestRedeemMissingDesktopDoesNotConsumeQuota(t *testing.T) {
	client := validRedeemClient()
	client.desktops = nil
	guard := NewGuard(DefaultPolicy(), SafetyState{}, GuardOptions{Now: time.Now})
	job := NewRedeemJob(client, guard, validRedeemPlan(), RedeemState{}, RedeemJobOptions{})
	if _, err := job.Run(context.Background()); err == nil {
		t.Fatal("expected missing desktop error")
	}
	if client.placeCalls != 0 || guard.Snapshot().DailyActions[ActionRedeem] != 0 {
		t.Fatalf("calls=%d safety=%#v", client.placeCalls, guard.Snapshot())
	}
}

func TestRedeemPendingStatePersistenceFailurePreventsOrder(t *testing.T) {
	client := validRedeemClient()
	guard := NewGuard(DefaultPolicy(), SafetyState{}, GuardOptions{Now: time.Now})
	job := NewRedeemJob(client, guard, validRedeemPlan(), RedeemState{}, RedeemJobOptions{
		SaveState: func(RedeemState) error { return errors.New("state disk failed") },
	})
	if _, err := job.Run(context.Background()); err == nil {
		t.Fatal("expected redeem state persistence error")
	}
	if client.placeCalls != 0 {
		t.Fatalf("placeOrder calls = %d, want 0", client.placeCalls)
	}
	if guard.Snapshot().DailyActions[ActionRedeem] != 1 {
		t.Fatalf("safety claim must remain consumed after state persistence failure: %#v", guard.Snapshot())
	}
}

func TestRedeemMaxTimesCapsSingleOrderPayload(t *testing.T) {
	client := validRedeemClient()
	client.points = 2000
	plan := validRedeemPlan()
	plan.MaxRedeemTimes = 2
	guard := NewGuard(DefaultPolicy(), SafetyState{}, GuardOptions{Now: time.Now})
	job := NewRedeemJob(client, guard, plan, RedeemState{}, RedeemJobOptions{})
	result, err := job.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Times != 2 || len(client.requests) != 1 || len(client.requests[0].SKUs) != 2 || client.requests[0].Points != 600 {
		t.Fatalf("result=%#v requests=%#v", result, client.requests)
	}
}
