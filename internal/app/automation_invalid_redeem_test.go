package app

import (
	"testing"
	"time"

	"github.com/uvwt/CtyunHelper/internal/automation"
	"github.com/uvwt/CtyunHelper/internal/ctyun/points"
)

func TestInvalidRedeemConfigStaysDisabledInModel(t *testing.T) {
	model := NewModel(State{Connection: ConnectionOnline})
	guard := automation.NewGuard(automation.DefaultPolicy(), automation.SafetyState{}, automation.GuardOptions{Now: time.Now})
	aiJob := automation.NewAIJob(
		&appAIPoints{tasks: []points.Task{{Name: automation.AITaskName, Status: automation.TaskDone}}},
		&appAIChat{}, guard, "你好",
	)
	client := &appPointsRedeemClient{value: 1000}
	redeemJob := automation.NewRedeemJob(client, guard, automation.RedeemPlan{Enabled: true}, automation.RedeemState{}, automation.RedeemJobOptions{})
	if _, err := NewTaskAutomationWithOptions(model, TaskAutomationOptions{AIJob: aiJob, RedeemJob: redeemJob}); err != nil {
		t.Fatal(err)
	}
	state := model.Snapshot()
	if state.RedeemEnabled || state.RedeemSummary == "" {
		t.Fatalf("invalid redeem config must be disabled with explanation: %#v", state)
	}
}
