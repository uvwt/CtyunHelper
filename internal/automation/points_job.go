package automation

import (
	"context"
	"fmt"
	"time"

	"github.com/uvwt/CtyunHelper/internal/ctyun/points"
)

const UsageTaskName = "使用1小时"

type PointsSnapshot struct {
	Points          int
	UsageTaskFound  bool
	UsageTaskStatus int
	UsageProgress   int
	AITaskFound     bool
	AITaskStatus    int
}

type pointsStatusClient interface {
	Tasks(context.Context) ([]points.Task, error)
	GeneralPoints(context.Context) (int, error)
}

type PointsJobOptions struct {
	WaitTimeout  time.Duration
	PollInterval time.Duration
}

type PointsJob struct {
	client       pointsStatusClient
	waitTimeout  time.Duration
	pollInterval time.Duration
}

func NewPointsJob(client pointsStatusClient, options PointsJobOptions) *PointsJob {
	waitTimeout := options.WaitTimeout
	if waitTimeout <= 0 {
		waitTimeout = 80 * time.Minute
	}
	pollInterval := options.PollInterval
	if pollInterval <= 0 {
		pollInterval = 5 * time.Minute
	}
	return &PointsJob{client: client, waitTimeout: waitTimeout, pollInterval: pollInterval}
}

// Refresh 只做一次只读查询，不等待“使用1小时”任务完成。
func (j *PointsJob) Refresh(ctx context.Context) (PointsSnapshot, error) {
	if j == nil || j.client == nil {
		return PointsSnapshot{}, fmt.Errorf("automation: 积分 Job 依赖未初始化")
	}
	snapshot, err := j.readTask(ctx)
	if err != nil {
		return PointsSnapshot{}, err
	}
	value, err := j.client.GeneralPoints(ctx)
	if err != nil {
		return snapshot, fmt.Errorf("automation: 查询通用积分: %w", err)
	}
	snapshot.Points = value
	return snapshot, nil
}

// WaitUsageAndRefresh 保留旧 pc_login.py 的语义：最多等待 80 分钟，每 5 分钟
// 只读检查一次“使用1小时”；超时不是业务失败，仍继续刷新当前通用积分。
func (j *PointsJob) WaitUsageAndRefresh(ctx context.Context, onUpdate func(PointsSnapshot)) (PointsSnapshot, error) {
	if j == nil || j.client == nil {
		return PointsSnapshot{}, fmt.Errorf("automation: 积分 Job 依赖未初始化")
	}
	deadline := time.Now().Add(j.waitTimeout)
	var snapshot PointsSnapshot
	for {
		current, err := j.readTask(ctx)
		if err != nil {
			return snapshot, err
		}
		snapshot = current
		if onUpdate != nil {
			onUpdate(snapshot)
		}
		if !snapshot.UsageTaskFound || snapshot.UsageTaskStatus == TaskDone || !time.Now().Before(deadline) {
			break
		}
		timer := time.NewTimer(j.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return snapshot, ctx.Err()
		case <-timer.C:
		}
	}
	value, err := j.client.GeneralPoints(ctx)
	if err != nil {
		return snapshot, fmt.Errorf("automation: 查询通用积分: %w", err)
	}
	snapshot.Points = value
	if onUpdate != nil {
		onUpdate(snapshot)
	}
	return snapshot, nil
}

func (j *PointsJob) readTask(ctx context.Context) (PointsSnapshot, error) {
	tasks, err := j.client.Tasks(ctx)
	if err != nil {
		return PointsSnapshot{}, fmt.Errorf("automation: 查询积分任务: %w", err)
	}
	snapshot := PointsSnapshot{}
	for _, task := range tasks {
		switch task.Name {
		case UsageTaskName:
			snapshot.UsageTaskFound = true
			snapshot.UsageTaskStatus = task.Status
			snapshot.UsageProgress = task.CurrentProgress
		case AITaskName:
			snapshot.AITaskFound = true
			snapshot.AITaskStatus = task.Status
		}
	}
	return snapshot, nil
}
