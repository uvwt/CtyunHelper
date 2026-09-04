package automation

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/uvwt/CtyunHelper/internal/ctyun/eai"
	"github.com/uvwt/CtyunHelper/internal/ctyun/points"
)

type fakeAIPoints struct {
	mu        sync.Mutex
	responses [][]points.Task
	calls     int
	err       error
}

func (f *fakeAIPoints) Tasks(context.Context) ([]points.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	if len(f.responses) == 0 {
		return nil, nil
	}
	index := f.calls
	if index >= len(f.responses) {
		index = len(f.responses) - 1
	}
	f.calls++
	return f.responses[index], nil
}

type fakeAIChat struct {
	calls int
	err   error
}

func (f *fakeAIChat) SendMessage(context.Context, string) (eai.ChatResult, error) {
	f.calls++
	if f.err != nil {
		return eai.ChatResult{}, f.err
	}
	return eai.ChatResult{EventCount: 1}, nil
}

func TestAIJobSendsOnceAndConfirmsTask(t *testing.T) {
	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.Local)
	guard := NewGuard(DefaultPolicy(), SafetyState{}, GuardOptions{Now: func() time.Time { return now }})
	pointClient := &fakeAIPoints{responses: [][]points.Task{
		{{Name: AITaskName, Status: 0}},
		{{Name: AITaskName, Status: TaskDone}},
	}}
	chat := &fakeAIChat{}
	job := NewAIJob(pointClient, chat, guard, "你好")
	job.verifyInterval = time.Millisecond
	job.verifyTimeout = 20 * time.Millisecond
	if err := job.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if chat.calls != 1 {
		t.Fatalf("chat calls = %d", chat.calls)
	}
	state := guard.Snapshot()
	if state.DailyActions[ActionAI] != 1 || state.ConsecutiveFailures != 0 {
		t.Fatalf("safety state = %#v", state)
	}
}

func TestAIJobDoesNotSpendQuotaWhenTaskAlreadyDone(t *testing.T) {
	guard := NewGuard(DefaultPolicy(), SafetyState{}, GuardOptions{Now: time.Now})
	pointClient := &fakeAIPoints{responses: [][]points.Task{{{Name: AITaskName, Status: TaskDone}}}}
	chat := &fakeAIChat{}
	if err := NewAIJob(pointClient, chat, guard, "你好").Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if chat.calls != 0 || guard.Snapshot().DailyActions[ActionAI] != 0 {
		t.Fatalf("chat=%d state=%#v", chat.calls, guard.Snapshot())
	}
}

func TestAIJobNeverRepeatsChatInsideVerificationLoop(t *testing.T) {
	guard := NewGuard(DefaultPolicy(), SafetyState{}, GuardOptions{Now: time.Now})
	pointClient := &fakeAIPoints{responses: [][]points.Task{{{Name: AITaskName, Status: 0}}}}
	chat := &fakeAIChat{}
	job := NewAIJob(pointClient, chat, guard, "你好")
	job.verifyInterval = time.Millisecond
	job.verifyTimeout = 4 * time.Millisecond
	if err := job.Run(context.Background()); err == nil {
		t.Fatal("expected verification timeout")
	}
	if chat.calls != 1 {
		t.Fatalf("chat calls = %d, want 1", chat.calls)
	}
	if guard.Snapshot().DailyActions[ActionAI] != 1 {
		t.Fatalf("safety state = %#v", guard.Snapshot())
	}
}

func TestAIJobDailyQuotaStopsThirdFailedChat(t *testing.T) {
	guard := NewGuard(DefaultPolicy(), SafetyState{}, GuardOptions{Now: time.Now})
	pointClient := &fakeAIPoints{responses: [][]points.Task{{{Name: AITaskName, Status: 0}}}}
	chat := &fakeAIChat{err: errors.New("chat failed")}
	job := NewAIJob(pointClient, chat, guard, "你好")
	for i := 0; i < 2; i++ {
		if err := job.Run(context.Background()); err == nil {
			t.Fatal("expected chat failure")
		}
	}
	if err := job.Run(context.Background()); err == nil {
		t.Fatal("third run should be blocked by daily quota")
	}
	if chat.calls != 2 {
		t.Fatalf("chat calls = %d, want 2", chat.calls)
	}
}

func TestAIJobDoesNotSendWhenQuotaPersistenceFails(t *testing.T) {
	guard := NewGuard(DefaultPolicy(), SafetyState{}, GuardOptions{
		Now:  time.Now,
		Save: func(SafetyState) error { return errors.New("disk failed") },
	})
	pointClient := &fakeAIPoints{responses: [][]points.Task{{{Name: AITaskName, Status: 0}}}}
	chat := &fakeAIChat{}
	if err := NewAIJob(pointClient, chat, guard, "你好").Run(context.Background()); err == nil {
		t.Fatal("expected quota persistence failure")
	}
	if chat.calls != 0 {
		t.Fatalf("chat calls = %d, want 0", chat.calls)
	}
}
