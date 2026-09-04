package app

import (
	"context"
	"fmt"
	"sync"

	"github.com/uvwt/CtyunHelper/internal/automation"
)

const (
	aiJobName     = "ai_task"
	pointsJobName = "points_refresh"
	redeemJobName = "redeem_task"
)

type TaskAutomationOptions struct {
	AIJob     *automation.AIJob
	PointsJob *automation.PointsJob
	RedeemJob *automation.RedeemJob
}

// TaskAutomation 负责把业务 Job 接到 Scheduler 和 App Model。
// Scheduler 只管时间与同 Job 不重入；pointsMu 额外保证积分刷新和兑换
// 两条链不会互相穿插，UI 只观察 Model，不直接调用协议 Client。
type TaskAutomation struct {
	model      *Model
	scheduler  *automation.Scheduler
	pointsJob  *automation.PointsJob
	redeemJob  *automation.RedeemJob
	pointsMu   sync.Mutex
	activityMu sync.RWMutex
}

// NewTaskAutomation 保留 AI-only 构造方式，方便协议/调度单测和后续轻量调用。
func NewTaskAutomation(model *Model, aiJob *automation.AIJob) (*TaskAutomation, error) {
	return NewTaskAutomationWithOptions(model, TaskAutomationOptions{AIJob: aiJob})
}

func NewTaskAutomationWithOptions(model *Model, options TaskAutomationOptions) (*TaskAutomation, error) {
	if model == nil || options.AIJob == nil {
		return nil, fmt.Errorf("app: 自动任务依赖未初始化")
	}
	value := &TaskAutomation{model: model, pointsJob: options.PointsJob, redeemJob: options.RedeemJob}
	value.scheduler = automation.NewScheduler(automation.SchedulerOptions{OnState: value.applyJobState})

	if err := value.scheduler.Register(automation.JobConfig{
		Name:  aiJobName,
		Times: []automation.ClockTime{{Hour: 3, Minute: 0}, {Hour: 20, Minute: 0}},
		Run: func(ctx context.Context) error {
			value.activityMu.RLock()
			defer value.activityMu.RUnlock()
			state := model.Snapshot()
			if state.AutomationPaused || state.Connection == ConnectionAuth || state.Connection == ConnectionDeviceBind {
				return nil
			}
			return options.AIJob.Run(ctx)
		},
	}); err != nil {
		return nil, err
	}

	if options.PointsJob != nil {
		if err := value.scheduler.Register(automation.JobConfig{
			Name:  pointsJobName,
			Times: []automation.ClockTime{{Hour: 4, Minute: 0}, {Hour: 6, Minute: 0}},
			Run: func(ctx context.Context) error {
				value.activityMu.RLock()
				defer value.activityMu.RUnlock()
				state := model.Snapshot()
				if state.Connection == ConnectionAuth || state.Connection == ConnectionDeviceBind {
					return nil
				}
				value.pointsMu.Lock()
				defer value.pointsMu.Unlock()
				snapshot, err := options.PointsJob.Refresh(ctx)
				if err == nil {
					value.applyPointsSnapshot(snapshot)
				}
				return err
			},
		}); err != nil {
			return nil, err
		}
	}

	if options.RedeemJob != nil {
		value.UpdateAccount(model.Snapshot().Account)
		if err := value.scheduler.Register(automation.JobConfig{
			Name:  redeemJobName,
			Times: []automation.ClockTime{{Hour: 4, Minute: 5}, {Hour: 6, Minute: 5}},
			Run: func(ctx context.Context) error {
				value.activityMu.RLock()
				defer value.activityMu.RUnlock()
				state := model.Snapshot()
				if state.AutomationPaused || state.Connection == ConnectionAuth || state.Connection == ConnectionDeviceBind || !state.RedeemEnabled {
					return nil
				}
				value.pointsMu.Lock()
				defer value.pointsMu.Unlock()

				if options.PointsJob != nil {
					snapshot, err := options.PointsJob.WaitUsageAndRefresh(ctx, value.applyPointsSnapshot)
					if err != nil {
						return err
					}
					value.applyPointsSnapshot(snapshot)
				}
				result, err := value.redeemJob.Run(ctx)
				value.applyRedeemResult(result, err)
				return err
			},
		}); err != nil {
			return nil, err
		}
	}
	return value, nil
}

func (a *TaskAutomation) Start(ctx context.Context) {
	if a == nil || a.scheduler == nil {
		return
	}
	a.scheduler.Start(ctx)
	// 启动时只做一次只读积分刷新，不触发兑换；这样当天 04:00/06:00 已错过
	// 也能尽快把余额和“使用1小时”状态显示到 UI。
	if a.pointsJob != nil {
		go func() { _ = a.RunPoints(ctx) }()
	}
}

func (a *TaskAutomation) RunAI(ctx context.Context) error {
	if a == nil || a.scheduler == nil {
		return fmt.Errorf("app: 自动任务未初始化")
	}
	return a.scheduler.RunNow(ctx, aiJobName)
}

func (a *TaskAutomation) RunPoints(ctx context.Context) error {
	if a == nil || a.scheduler == nil || a.pointsJob == nil {
		return fmt.Errorf("app: 积分刷新任务未初始化")
	}
	return a.scheduler.RunNow(ctx, pointsJobName)
}

func (a *TaskAutomation) RunRedeem(ctx context.Context) error {
	if a == nil || a.scheduler == nil || a.redeemJob == nil {
		return fmt.Errorf("app: 兑换任务未初始化")
	}
	if !a.model.Snapshot().RedeemEnabled {
		return fmt.Errorf("app: 自动兑换未启用或当前账号不匹配")
	}
	return a.scheduler.RunNow(ctx, redeemJobName)
}

func (a *TaskAutomation) UpdateAccount(account string) {
	if a == nil || a.model == nil || a.redeemJob == nil {
		return
	}
	redeemState := a.redeemJob.Snapshot()
	validationErr := a.redeemJob.Validate()
	accountMatches := a.redeemJob.Account() != "" && a.redeemJob.Account() == account
	pending := redeemState.LastAttemptStatus == automation.RedeemAttemptPending
	a.model.Update(func(state *State) {
		state.RedeemEnabled = a.redeemJob.Enabled() && validationErr == nil && accountMatches && !pending
		switch {
		case !a.redeemJob.Enabled():
			state.RedeemSummary = "未启用"
		case validationErr != nil:
			state.RedeemSummary = "配置错误：" + validationErr.Error()
		case !accountMatches:
			state.RedeemSummary = "兑换配置属于其他账号，请重新配置"
		case redeemState.LastAttemptStatus == automation.RedeemAttemptPending:
			state.RedeemSummary = "上次兑换结果不确定，已停止自动兑换"
		case redeemState.LastSuccessDate != "":
			state.RedeemSummary = fmt.Sprintf("最近成功：%s，%d 次 / %d 积分", redeemState.LastSuccessDate, redeemState.LastRedeemTimes, redeemState.LastPointsSpent)
		default:
			state.RedeemSummary = "等待兑换计划"
		}
	})
}

func (a *TaskAutomation) applyPointsSnapshot(snapshot automation.PointsSnapshot) {
	a.model.Update(func(state *State) {
		state.Points = snapshot.Points
		state.UsageTask = UsageTaskStatus{
			Found: snapshot.UsageTaskFound, Status: snapshot.UsageTaskStatus, Progress: snapshot.UsageProgress,
		}
	})
}

func (a *TaskAutomation) applyRedeemResult(result automation.RedeemResult, runErr error) {
	a.model.Update(func(state *State) {
		if result.Redeemed {
			state.Points = result.PointsBefore - result.PointsSpent
			if state.Points < 0 {
				state.Points = 0
			}
			state.RedeemSummary = fmt.Sprintf("成功兑换 %d 次，消耗 %d 积分", result.Times, result.PointsSpent)
		} else if result.SkippedReason != "" {
			state.RedeemSummary = result.SkippedReason
		} else if runErr != nil {
			state.RedeemSummary = runErr.Error()
		}
	})
}

func (a *TaskAutomation) applyJobState(state automation.JobState) {
	a.model.Update(func(current *State) {
		job := JobStatus{
			Running: state.Running, LastRun: state.LastRun, NextRun: state.NextRun, LastError: state.LastError,
		}
		switch state.Name {
		case aiJobName:
			current.AITask = job
		case pointsJobName:
			current.PointsTask = job
		case redeemJobName:
			current.RedeemTask = job
		}
	})
}
