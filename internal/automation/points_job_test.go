package automation

import (
	"context"
	"testing"
	"time"

	"github.com/uvwt/CtyunHelper/internal/ctyun/points"
)

type fakePointsStatus struct {
	tasks [][]points.Task
	calls int
	value int
}

func (f *fakePointsStatus) Tasks(context.Context) ([]points.Task, error) {
	if len(f.tasks) == 0 {
		return nil, nil
	}
	index := f.calls
	if index >= len(f.tasks) {
		index = len(f.tasks) - 1
	}
	f.calls++
	return f.tasks[index], nil
}

func (f *fakePointsStatus) GeneralPoints(context.Context) (int, error) { return f.value, nil }

func TestPointsRefreshReportsUsageAndTotalPoints(t *testing.T) {
	client := &fakePointsStatus{
		tasks: [][]points.Task{{
			{Name: LoginAITaskName, Status: TaskDone, CurrentProgress: 1},
			{Name: UsageTaskName, Status: 0, CurrentProgress: 35},
			{Name: AITaskName, Status: TaskDone, CurrentProgress: 1},
		}},
		value: 650,
	}
	job := NewPointsJob(client, PointsJobOptions{})
	snapshot, err := job.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Points != 650 || !snapshot.LoginAITaskFound || snapshot.LoginAITaskStatus != TaskDone || snapshot.LoginAIProgress != 1 || !snapshot.UsageTaskFound || snapshot.UsageTaskStatus != 0 || snapshot.UsageProgress != 35 || !snapshot.AITaskFound || snapshot.AITaskStatus != TaskDone || snapshot.AITaskProgress != 1 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestPointsWaitPollsUntilUsageTaskCompletes(t *testing.T) {
	client := &fakePointsStatus{
		tasks: [][]points.Task{
			{{Name: UsageTaskName, Status: 0, CurrentProgress: 59}},
			{{Name: UsageTaskName, Status: TaskDone, CurrentProgress: 60}},
		},
		value: 700,
	}
	job := NewPointsJob(client, PointsJobOptions{WaitTimeout: 50 * time.Millisecond, PollInterval: time.Millisecond})
	var updates []PointsSnapshot
	snapshot, err := job.WaitUsageAndRefresh(context.Background(), func(value PointsSnapshot) {
		updates = append(updates, value)
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.calls != 2 || snapshot.Points != 700 || snapshot.UsageTaskStatus != TaskDone || len(updates) < 3 {
		t.Fatalf("calls=%d snapshot=%#v updates=%#v", client.calls, snapshot, updates)
	}
}

func TestPointsWaitTimeoutStillRefreshesBalance(t *testing.T) {
	client := &fakePointsStatus{
		tasks: [][]points.Task{{{Name: UsageTaskName, Status: 0, CurrentProgress: 1}}},
		value: 88,
	}
	job := NewPointsJob(client, PointsJobOptions{WaitTimeout: 2 * time.Millisecond, PollInterval: time.Millisecond})
	snapshot, err := job.WaitUsageAndRefresh(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Points != 88 || snapshot.UsageTaskStatus != 0 || client.calls < 1 {
		t.Fatalf("snapshot=%#v calls=%d", snapshot, client.calls)
	}
}
