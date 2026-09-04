package app

import (
	"context"
	"testing"
	"time"

	"github.com/uvwt/CtyunHelper/internal/automation"
	"github.com/uvwt/CtyunHelper/internal/ctyun/points"
)

func TestTaskAutomationStartOnlyRefreshesAndNeverRedeemsImmediately(t *testing.T) {
	model := NewModel(State{Connection: ConnectionOnline})
	client := &appPointsRedeemClient{
		tasks: []points.Task{{Name: automation.UsageTaskName, Status: automation.TaskDone, CurrentProgress: 60}},
		value: 650,
	}
	tasks, guard := newAppAutomationForPoints(t, model, client, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tasks.Start(ctx)
	deadline := time.Now().Add(time.Second)
	for model.Snapshot().PointsTask.LastRun.IsZero() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if model.Snapshot().PointsTask.LastRun.IsZero() {
		t.Fatal("startup points refresh did not run")
	}
	if client.placeCalls != 0 || guard.Snapshot().DailyActions[automation.ActionRedeem] != 0 {
		t.Fatalf("startup must never redeem: calls=%d safety=%#v", client.placeCalls, guard.Snapshot())
	}
}
