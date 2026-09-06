package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/uvwt/CtyunHelper/internal/automation"
	"github.com/uvwt/CtyunHelper/internal/ctyun/points"
)

type appPointsRedeemClient struct {
	tasks      []points.Task
	value      int
	placeCalls int
}

func (c *appPointsRedeemClient) Tasks(context.Context) ([]points.Task, error) {
	return append([]points.Task(nil), c.tasks...), nil
}
func (c *appPointsRedeemClient) GeneralPoints(context.Context) (int, error) { return c.value, nil }
func (c *appPointsRedeemClient) Products(context.Context) ([]points.ProductMall, error) {
	return []points.ProductMall{{Series: []points.ProductSeries{{SKUs: []points.ProductSKU{{
		ProductID: 99, ProductName: "奖励", ProductType: "gift", CostPoints: 300, Status: 2,
	}}}}}}, nil
}
func (c *appPointsRedeemClient) Desktops(context.Context) ([]points.Desktop, error) {
	return []points.Desktop{{DesktopID: 42, DesktopName: "云电脑"}}, nil
}
func (c *appPointsRedeemClient) PlaceOrder(context.Context, points.OrderRequest) (json.RawMessage, error) {
	c.placeCalls++
	return json.RawMessage(`{"ok":true}`), nil
}

func newAppAutomationForPoints(t *testing.T, model *Model, client *appPointsRedeemClient, redeemEnabled bool) (*TaskAutomation, *automation.Guard) {
	t.Helper()
	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.Local)
	guard := automation.NewGuard(automation.DefaultPolicy(), automation.SafetyState{}, automation.GuardOptions{Now: func() time.Time { return now }})
	aiJob := automation.NewAIJob(
		&appAIPoints{tasks: []points.Task{{Name: automation.AITaskName, Status: automation.TaskDone}}},
		&appAIChat{}, guard, "你好",
	)
	pointsJob := automation.NewPointsJob(client, automation.PointsJobOptions{WaitTimeout: time.Millisecond, PollInterval: time.Millisecond})
	redeemJob := automation.NewRedeemJob(client, guard, automation.RedeemPlan{
		Enabled: redeemEnabled, Account: "account", DesktopID: 42, ProductID: 99, ProductName: "奖励",
		ProductType: "gift", CostPoints: 300, ScheduleType: automation.RedeemScheduleDaily,
	}, automation.RedeemState{}, automation.RedeemJobOptions{Now: func() time.Time { return now }})
	value, err := NewTaskAutomationWithOptions(model, TaskAutomationOptions{
		AIJob: aiJob, PointsJob: pointsJob, RedeemJob: redeemJob,
	})
	if err != nil {
		t.Fatal(err)
	}
	return value, guard
}

func TestTaskAutomationRefreshPointsIsReadOnlyAndPublishesUsage(t *testing.T) {
	model := NewModel(State{Account: "account", Connection: ConnectionOnline})
	client := &appPointsRedeemClient{
		tasks: []points.Task{
			{Name: automation.LoginAITaskName, Status: automation.TaskDone, CurrentProgress: 1},
			{Name: automation.UsageTaskName, Status: automation.TaskDone, CurrentProgress: 3600},
			{Name: automation.AITaskName, Status: automation.TaskDone, CurrentProgress: 1},
		},
		value: 650,
	}
	tasks, guard := newAppAutomationForPoints(t, model, client, false)
	if err := tasks.RunPoints(context.Background()); err != nil {
		t.Fatal(err)
	}
	state := model.Snapshot()
	if state.Points != 650 || !state.LoginAITask.Found || state.LoginAITask.Status != automation.TaskDone || state.LoginAITask.Progress != 1 || !state.UsageTask.Found || state.UsageTask.Status != automation.TaskDone || state.UsageTask.Progress != 3600 || !state.AIPointsTask.Found || state.AIPointsTask.Status != automation.TaskDone || state.AIPointsTask.Progress != 1 || state.RedeemEnabled {
		t.Fatalf("state = %#v", state)
	}
	if client.placeCalls != 0 || guard.Snapshot().DailyActions[automation.ActionRedeem] != 0 {
		t.Fatalf("refresh must be read-only: calls=%d safety=%#v", client.placeCalls, guard.Snapshot())
	}
}

func TestTaskAutomationRedeemUpdatesModelAfterSingleOrder(t *testing.T) {
	model := NewModel(State{Account: "account", Connection: ConnectionOnline})
	client := &appPointsRedeemClient{
		tasks: []points.Task{{Name: automation.UsageTaskName, Status: automation.TaskDone, CurrentProgress: 60}},
		value: 650,
	}
	tasks, guard := newAppAutomationForPoints(t, model, client, true)
	if err := tasks.RunRedeem(context.Background()); err != nil {
		t.Fatal(err)
	}
	state := model.Snapshot()
	if client.placeCalls != 1 || state.Points != 50 || state.RedeemSummary == "" || !state.RedeemEnabled {
		t.Fatalf("calls=%d state=%#v", client.placeCalls, state)
	}
	if guard.Snapshot().DailyActions[automation.ActionRedeem] != 1 {
		t.Fatalf("safety = %#v", guard.Snapshot())
	}
}

func TestTaskAutomationManualRedeemReturnsWhenUsageTaskIsIncomplete(t *testing.T) {
	model := NewModel(State{Account: "account", Connection: ConnectionOnline})
	client := &appPointsRedeemClient{
		tasks: []points.Task{{Name: automation.UsageTaskName, Status: 0, CurrentProgress: 12}},
		value: 650,
	}
	tasks, guard := newAppAutomationForPoints(t, model, client, true)
	if err := tasks.RunRedeem(context.Background()); err != nil {
		t.Fatal(err)
	}
	state := model.Snapshot()
	if client.placeCalls != 0 || guard.Snapshot().DailyActions[automation.ActionRedeem] != 0 {
		t.Fatalf("manual check must not redeem early: calls=%d safety=%#v", client.placeCalls, guard.Snapshot())
	}
	if state.Points != 650 || state.RedeemSummary != "使用1小时任务未完成，暂不兑换" {
		t.Fatalf("state = %#v", state)
	}
}

func TestTaskAutomationUsagePollingDoesNotOverwritePointsWithZero(t *testing.T) {
	model := NewModel(State{Points: 777})
	tasks := &TaskAutomation{model: model}
	tasks.applyPointsTaskSnapshot(automation.PointsSnapshot{
		UsageTaskFound: true, UsageTaskStatus: 0, UsageProgress: 12,
	})
	state := model.Snapshot()
	if state.Points != 777 || !state.UsageTask.Found || state.UsageTask.Progress != 12 {
		t.Fatalf("state = %#v", state)
	}
}
