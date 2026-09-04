package app

import (
	"context"
	"testing"
	"time"

	"github.com/uvwt/CtyunHelper/internal/automation"
	"github.com/uvwt/CtyunHelper/internal/ctyun/eai"
	"github.com/uvwt/CtyunHelper/internal/ctyun/points"
)

type appAIPoints struct {
	tasks []points.Task
}

func (p *appAIPoints) Tasks(context.Context) ([]points.Task, error) {
	return p.tasks, nil
}

type appAIChat struct {
	calls int
}

func (c *appAIChat) SendMessage(context.Context, string) (eai.ChatResult, error) {
	c.calls++
	return eai.ChatResult{EventCount: 1}, nil
}

func TestTaskAutomationPublishesScheduleStateToModel(t *testing.T) {
	model := NewModel(State{Connection: ConnectionOnline})
	guard := automation.NewGuard(automation.DefaultPolicy(), automation.SafetyState{}, automation.GuardOptions{Now: time.Now})
	job := automation.NewAIJob(
		&appAIPoints{tasks: []points.Task{{Name: automation.AITaskName, Status: automation.TaskDone}}},
		&appAIChat{},
		guard,
		"你好",
	)
	tasks, err := NewTaskAutomation(model, job)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tasks.Start(ctx)

	deadline := time.Now().Add(time.Second)
	for model.Snapshot().AITask.NextRun.IsZero() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	state := model.Snapshot().AITask
	if state.NextRun.IsZero() || state.Running {
		t.Fatalf("AI task state = %#v", state)
	}
}

func TestTaskAutomationSkipsAIWhenLoginIsRequired(t *testing.T) {
	model := NewModel(State{Connection: ConnectionAuth})
	chat := &appAIChat{}
	guard := automation.NewGuard(automation.DefaultPolicy(), automation.SafetyState{}, automation.GuardOptions{Now: time.Now})
	job := automation.NewAIJob(
		&appAIPoints{tasks: []points.Task{{Name: automation.AITaskName, Status: 0}}},
		chat,
		guard,
		"你好",
	)
	tasks, err := NewTaskAutomation(model, job)
	if err != nil {
		t.Fatal(err)
	}
	if err := tasks.RunAI(context.Background()); err != nil {
		t.Fatal(err)
	}
	if chat.calls != 0 || guard.Snapshot().DailyActions[automation.ActionAI] != 0 {
		t.Fatalf("chat calls=%d safety=%#v", chat.calls, guard.Snapshot())
	}
}
