package automation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/uvwt/CtyunHelper/internal/ctyun/eai"
	"github.com/uvwt/CtyunHelper/internal/ctyun/points"
)

const (
	AITaskName = "与AI对话1次"
	TaskDone   = 2
)

type aiPoints interface {
	Tasks(context.Context) ([]points.Task, error)
}

type aiChat interface {
	SendMessage(context.Context, string) (eai.ChatResult, error)
}

type AIJob struct {
	points aiPoints
	chat   aiChat
	guard  *Guard

	prompt         string
	verifyTimeout  time.Duration
	verifyInterval time.Duration
}

func NewAIJob(pointsClient aiPoints, chatClient aiChat, guard *Guard, prompt string) *AIJob {
	return &AIJob{
		points:         pointsClient,
		chat:           chatClient,
		guard:          guard,
		prompt:         prompt,
		verifyTimeout:  3 * time.Minute,
		verifyInterval: 30 * time.Second,
	}
}

// Run 只在积分任务确实未完成时发送一次 AI 请求。每日额度在真正发送前占用；
// 发送成功后只轮询任务状态，不会因为积分延迟而在同一轮重复对话。
func (j *AIJob) Run(ctx context.Context) error {
	if j.points == nil || j.chat == nil || j.guard == nil {
		return fmt.Errorf("automation: AI Job 依赖未初始化")
	}
	tasks, err := j.points.Tasks(ctx)
	if err != nil {
		return j.recordFailure(fmt.Errorf("automation: 查询 AI 积分任务: %w", err))
	}
	task, exists := findTask(tasks, AITaskName)
	if !exists || task.Status == TaskDone {
		return nil
	}

	// Claim 会先持久化当日额度；保存失败时不会继续发起真正的 EAI 请求。
	if err := j.guard.Claim(ActionAI); err != nil {
		return fmt.Errorf("automation: AI 任务被保守策略阻止: %w", err)
	}
	if _, err := j.chat.SendMessage(ctx, j.prompt); err != nil {
		return j.recordFailure(fmt.Errorf("automation: EAI 对话失败: %w", err))
	}

	deadline := time.Now().Add(j.verifyTimeout)
	for {
		tasks, err := j.points.Tasks(ctx)
		if err != nil {
			return j.recordFailure(fmt.Errorf("automation: 验证 AI 积分任务: %w", err))
		}
		task, exists = findTask(tasks, AITaskName)
		if !exists || task.Status == TaskDone {
			return j.guard.RecordSuccess()
		}
		if !time.Now().Before(deadline) {
			return j.recordFailure(fmt.Errorf("automation: AI 对话成功，但积分任务在 %s 内未确认完成", j.verifyTimeout))
		}
		timer := time.NewTimer(j.verifyInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (j *AIJob) recordFailure(actionErr error) error {
	if safetyErr := j.guard.RecordFailure(); safetyErr != nil {
		return errors.Join(actionErr, safetyErr)
	}
	return actionErr
}

func findTask(tasks []points.Task, name string) (points.Task, bool) {
	for _, task := range tasks {
		if task.Name == name {
			return task, true
		}
	}
	return points.Task{}, false
}
