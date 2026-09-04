package automation

import (
	"context"
	"testing"
	"time"
)

func TestSchedulerPublishesRunningAndFinishedStates(t *testing.T) {
	fixedNow := time.Date(2026, 9, 4, 10, 0, 0, 0, time.Local)
	states := make(chan JobState, 4)
	scheduler := NewScheduler(SchedulerOptions{
		Now:     func() time.Time { return fixedNow },
		OnState: func(state JobState) { states <- state },
	})
	if err := scheduler.Register(JobConfig{
		Name:  "ai",
		Times: []ClockTime{{Hour: 20}},
		Run:   func(context.Context) error { return nil },
	}); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.RunNow(context.Background(), "ai"); err != nil {
		t.Fatal(err)
	}

	running := <-states
	finished := <-states
	if !running.Running || !running.LastRun.Equal(fixedNow) {
		t.Fatalf("running = %#v", running)
	}
	if finished.Running || !finished.LastRun.Equal(fixedNow) || finished.LastError != "" {
		t.Fatalf("finished = %#v", finished)
	}
}

func TestSchedulerPublishesNextRunAfterStart(t *testing.T) {
	fixedNow := time.Date(2026, 9, 4, 10, 0, 0, 0, time.Local)
	states := make(chan JobState, 4)
	scheduler := NewScheduler(SchedulerOptions{
		Now:     func() time.Time { return fixedNow },
		OnState: func(state JobState) { states <- state },
	})
	if err := scheduler.Register(JobConfig{
		Name:  "ai",
		Times: []ClockTime{{Hour: 20}},
		Run:   func(context.Context) error { return nil },
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scheduler.Start(ctx)

	select {
	case state := <-states:
		want := time.Date(2026, 9, 4, 20, 0, 0, 0, time.Local)
		if !state.NextRun.Equal(want) {
			t.Fatalf("next = %s, want %s", state.NextRun, want)
		}
	case <-time.After(time.Second):
		t.Fatal("scheduler did not publish initial next run")
	}
}
