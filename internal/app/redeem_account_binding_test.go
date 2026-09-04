package app

import (
	"context"
	"testing"
	"time"

	"github.com/uvwt/CtyunHelper/internal/automation"
	"github.com/uvwt/CtyunHelper/internal/ctyun/points"
)

func TestRedeemPlanIsBoundToConfiguredAccount(t *testing.T) {
	model := NewModel(State{Account: "account", Connection: ConnectionOnline})
	client := &appPointsRedeemClient{
		tasks: []points.Task{{Name: automation.UsageTaskName, Status: automation.TaskDone, CurrentProgress: 60}},
		value: 650,
	}
	tasks, guard := newAppAutomationForPoints(t, model, client, true)
	if !model.Snapshot().RedeemEnabled {
		t.Fatal("redeem should be enabled for configured account")
	}

	tasks.UpdateAccount("other-account")
	state := model.Snapshot()
	if state.RedeemEnabled || state.RedeemSummary == "" {
		t.Fatalf("redeem must be disabled after account switch: %#v", state)
	}
	if err := tasks.RunRedeem(context.Background()); err == nil {
		t.Fatal("cross-account redeem should be rejected")
	}
	if client.placeCalls != 0 || guard.Snapshot().DailyActions[automation.ActionRedeem] != 0 {
		t.Fatalf("cross-account check must not consume anything: calls=%d safety=%#v", client.placeCalls, guard.Snapshot())
	}

	tasks.UpdateAccount("account")
	if !model.Snapshot().RedeemEnabled {
		t.Fatal("redeem should be re-enabled when returning to configured account")
	}
}

func TestRedeemSchedulerRechecksAccountAfterProfileWriteLock(t *testing.T) {
	model := NewModel(State{Account: "account", Connection: ConnectionOnline})
	client := &appPointsRedeemClient{
		tasks: []points.Task{{Name: automation.UsageTaskName, Status: automation.TaskDone, CurrentProgress: 60}},
		value: 650,
	}
	tasks, guard := newAppAutomationForPoints(t, model, client, true)

	// 模拟换号先取得 Profile 写锁，随后把当前账号更新成不匹配账号；兑换
	// Scheduler 必须在获得读锁后重新读取 Model，因此不能沿用锁前旧状态。
	tasks.activityMu.Lock()
	done := make(chan error, 1)
	go func() { done <- tasks.RunRedeem(context.Background()) }()
	time.Sleep(5 * time.Millisecond)
	tasks.UpdateAccount("other-account")
	tasks.activityMu.Unlock()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("redeem scheduler did not return")
	}
	if client.placeCalls != 0 || guard.Snapshot().DailyActions[automation.ActionRedeem] != 0 {
		t.Fatalf("stale account state triggered redeem: calls=%d safety=%#v", client.placeCalls, guard.Snapshot())
	}
}
