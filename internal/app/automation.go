package app

import (
	"context"
	"fmt"

	"github.com/uvwt/CtyunHelper/internal/automation"
)

const aiJobName = "ai_task"

// TaskAutomation 负责把业务 Job 接到 Scheduler 和 App Model。
// Scheduler 只管时间与不重入；Job 自己处理积分条件与 Safety 额度；UI 只观察 Model。
type TaskAutomation struct {
	model     *Model
	scheduler *automation.Scheduler
}

func NewTaskAutomation(model *Model, aiJob *automation.AIJob) (*TaskAutomation, error) {
	if model == nil || aiJob == nil {
		return nil, fmt.Errorf("app: 自动任务依赖未初始化")
	}
	value := &TaskAutomation{model: model}
	value.scheduler = automation.NewScheduler(automation.SchedulerOptions{
		OnState: value.applyJobState,
	})
	if err := value.scheduler.Register(automation.JobConfig{
		Name: aiJobName,
		Times: []automation.ClockTime{
			{Hour: 3, Minute: 0},
			{Hour: 20, Minute: 0},
		},
		Run: func(ctx context.Context) error {
			state := model.Snapshot()
			if state.AutomationPaused || state.Connection == ConnectionAuth || state.Connection == ConnectionDeviceBind {
				return nil
			}
			return aiJob.Run(ctx)
		},
	}); err != nil {
		return nil, err
	}
	return value, nil
}

func (a *TaskAutomation) Start(ctx context.Context) {
	if a != nil && a.scheduler != nil {
		a.scheduler.Start(ctx)
	}
}

func (a *TaskAutomation) RunAI(ctx context.Context) error {
	if a == nil || a.scheduler == nil {
		return fmt.Errorf("app: 自动任务未初始化")
	}
	return a.scheduler.RunNow(ctx, aiJobName)
}

func (a *TaskAutomation) applyJobState(state automation.JobState) {
	if state.Name != aiJobName {
		return
	}
	a.model.Update(func(current *State) {
		current.AITask = JobStatus{
			Running:   state.Running,
			LastRun:   state.LastRun,
			NextRun:   state.NextRun,
			LastError: state.LastError,
		}
	})
}
