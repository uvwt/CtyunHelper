package automation

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestNextDailyRunChoosesTodayThenTomorrow(t *testing.T) {
	location := time.FixedZone("CST", 8*3600)
	times := []ClockTime{{Hour: 3}, {Hour: 20}}
	now := time.Date(2026, 9, 4, 5, 30, 0, 0, location)
	if got, want := nextDailyRun(now, times), time.Date(2026, 9, 4, 20, 0, 0, 0, location); !got.Equal(want) {
		t.Fatalf("next = %s, want %s", got, want)
	}
	now = time.Date(2026, 9, 4, 21, 0, 0, 0, location)
	if got, want := nextDailyRun(now, times), time.Date(2026, 9, 5, 3, 0, 0, 0, location); !got.Equal(want) {
		t.Fatalf("next = %s, want %s", got, want)
	}
}

func TestSchedulerRejectsOverlappingRunNow(t *testing.T) {
	fixedNow := time.Date(2026, 9, 4, 10, 0, 0, 0, time.Local)
	scheduler := NewScheduler(SchedulerOptions{Now: func() time.Time { return fixedNow }})
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	if err := scheduler.Register(JobConfig{
		Name:  "ai",
		Times: []ClockTime{{Hour: 3}},
		Run: func(context.Context) error {
			calls.Add(1)
			close(started)
			<-release
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- scheduler.RunNow(context.Background(), "ai") }()
	<-started
	if err := scheduler.RunNow(context.Background(), "ai"); !errors.Is(err, ErrJobAlreadyRunning) {
		t.Fatalf("overlapping RunNow error = %v", err)
	}
	state, ok := scheduler.State("ai")
	if !ok || !state.Running || !state.LastRun.Equal(fixedNow) {
		t.Fatalf("running state = %#v, ok=%v", state, ok)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestSchedulerStoresLastError(t *testing.T) {
	scheduler := NewScheduler(SchedulerOptions{Now: time.Now})
	if err := scheduler.Register(JobConfig{
		Name:  "points",
		Times: []ClockTime{{Hour: 4}},
		Run:   func(context.Context) error { return errors.New("failed") },
	}); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.RunNow(context.Background(), "points"); err == nil {
		t.Fatal("expected job error")
	}
	state, _ := scheduler.State("points")
	if state.Running || state.LastError != "failed" {
		t.Fatalf("state = %#v", state)
	}
}

func TestSchedulerRunNowWithUsesAlternateRunner(t *testing.T) {
	scheduler := NewScheduler(SchedulerOptions{Now: time.Now})
	var scheduledCalls atomic.Int32
	var manualCalls atomic.Int32
	if err := scheduler.Register(JobConfig{
		Name:  "redeem",
		Times: []ClockTime{{Hour: 4}},
		Run: func(context.Context) error {
			scheduledCalls.Add(1)
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.RunNowWith(context.Background(), "redeem", func(context.Context) error {
		manualCalls.Add(1)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if scheduledCalls.Load() != 0 || manualCalls.Load() != 1 {
		t.Fatalf("scheduled=%d manual=%d", scheduledCalls.Load(), manualCalls.Load())
	}
	state, ok := scheduler.State("redeem")
	if !ok || state.Running || state.LastRun.IsZero() || state.LastError != "" {
		t.Fatalf("state = %#v, ok=%v", state, ok)
	}
}
