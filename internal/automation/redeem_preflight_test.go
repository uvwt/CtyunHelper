package automation

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRedeemNetworkFailureBeforeOrderDoesNotConsumeQuota(t *testing.T) {
	client := validRedeemClient()
	client.pointsErr = errors.New("network down")
	guard := NewGuard(DefaultPolicy(), SafetyState{}, GuardOptions{Now: time.Now})
	job := NewRedeemJob(client, guard, validRedeemPlan(), RedeemState{}, RedeemJobOptions{})
	if _, err := job.Run(context.Background()); err == nil {
		t.Fatal("expected points network error")
	}
	if client.placeCalls != 0 || guard.Snapshot().DailyActions[ActionRedeem] != 0 {
		t.Fatalf("calls=%d safety=%#v", client.placeCalls, guard.Snapshot())
	}
}

func TestRedeemMissingProductDoesNotConsumeQuota(t *testing.T) {
	client := validRedeemClient()
	client.products = nil
	guard := NewGuard(DefaultPolicy(), SafetyState{}, GuardOptions{Now: time.Now})
	job := NewRedeemJob(client, guard, validRedeemPlan(), RedeemState{}, RedeemJobOptions{})
	if _, err := job.Run(context.Background()); err == nil {
		t.Fatal("expected missing product error")
	}
	if client.placeCalls != 0 || guard.Snapshot().DailyActions[ActionRedeem] != 0 {
		t.Fatalf("calls=%d safety=%#v", client.placeCalls, guard.Snapshot())
	}
}
