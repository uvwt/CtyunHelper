package automation

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

var ErrJobAlreadyRunning = errors.New("automation: job 已在运行")

type ClockTime struct {
	Hour   int
	Minute int
}

type JobConfig struct {
	Name  string
	Times []ClockTime
	Run   func(context.Context) error
}

type JobState struct {
	Name      string
	Running   bool
	LastRun   time.Time
	NextRun   time.Time
	LastError string
}

type scheduledJob struct {
	config JobConfig
	mu     sync.Mutex
	state  JobState
}

// Scheduler 只负责“何时运行”和“同一 Job 不重入”。业务是否应该执行、
// 每日额度和失败冷却由具体 Job/Safety Guard 决定，避免调度层混入业务规则。
type SchedulerOptions struct {
	Now     func() time.Time
	OnState func(JobState)
}

type Scheduler struct {
	mu      sync.RWMutex
	jobs    map[string]*scheduledJob
	started bool
	now     func() time.Time
	onState func(JobState)
}

func NewScheduler(options SchedulerOptions) *Scheduler {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Scheduler{jobs: make(map[string]*scheduledJob), now: now, onState: options.OnState}
}

func (s *Scheduler) Register(config JobConfig) error {
	if config.Name == "" || config.Run == nil {
		return fmt.Errorf("automation: Job 名称和 Run 不能为空")
	}
	if len(config.Times) == 0 {
		return fmt.Errorf("automation: Job %s 没有调度时间", config.Name)
	}
	for _, value := range config.Times {
		if value.Hour < 0 || value.Hour > 23 || value.Minute < 0 || value.Minute > 59 {
			return fmt.Errorf("automation: Job %s 时间无效 %02d:%02d", config.Name, value.Hour, value.Minute)
		}
	}
	config.Times = append([]ClockTime(nil), config.Times...)
	sort.Slice(config.Times, func(i, j int) bool {
		if config.Times[i].Hour == config.Times[j].Hour {
			return config.Times[i].Minute < config.Times[j].Minute
		}
		return config.Times[i].Hour < config.Times[j].Hour
	})

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return fmt.Errorf("automation: Scheduler 启动后不能再注册 Job")
	}
	if _, exists := s.jobs[config.Name]; exists {
		return fmt.Errorf("automation: Job %s 已存在", config.Name)
	}
	s.jobs[config.Name] = &scheduledJob{config: config, state: JobState{Name: config.Name}}
	return nil
}

func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	jobs := make([]*scheduledJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job)
	}
	s.mu.Unlock()

	for _, job := range jobs {
		go s.runSchedule(ctx, job)
	}
}

func (s *Scheduler) RunNow(ctx context.Context, name string) error {
	return s.runNow(ctx, name, nil)
}

// RunNowWith 复用已注册 Job 的运行状态与不重入保护，但允许手动触发时
// 使用一条更适合交互场景的执行路径。定时调度仍始终执行注册时的 Run。
func (s *Scheduler) RunNowWith(ctx context.Context, name string, run func(context.Context) error) error {
	if run == nil {
		return fmt.Errorf("automation: 手动 Job Run 不能为空")
	}
	return s.runNow(ctx, name, run)
}

func (s *Scheduler) runNow(ctx context.Context, name string, run func(context.Context) error) error {
	s.mu.RLock()
	job := s.jobs[name]
	s.mu.RUnlock()
	if job == nil {
		return fmt.Errorf("automation: 未找到 Job %s", name)
	}
	if run == nil {
		run = job.config.Run
	}
	return s.runJob(ctx, job, s.now(), run)
}

func (s *Scheduler) State(name string) (JobState, bool) {
	s.mu.RLock()
	job := s.jobs[name]
	s.mu.RUnlock()
	if job == nil {
		return JobState{}, false
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	return job.state, true
}

func (s *Scheduler) runSchedule(ctx context.Context, job *scheduledJob) {
	for ctx.Err() == nil {
		now := s.now()
		next := nextDailyRun(now, job.config.Times)
		state := job.update(func(state *JobState) {
			state.NextRun = next
		})
		s.publish(state)

		timer := time.NewTimer(next.Sub(now))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			// Windows 从睡眠恢复后 Timer 最多触发一次；下一轮会基于当前时间
			// 重新计算，不补跑所有错过的时间点，避免唤醒后请求风暴。
			_ = s.runJob(ctx, job, s.now(), job.config.Run)
		}
	}
}

func (s *Scheduler) runJob(ctx context.Context, job *scheduledJob, now time.Time, run func(context.Context) error) error {
	job.mu.Lock()
	if job.state.Running {
		job.mu.Unlock()
		return ErrJobAlreadyRunning
	}
	job.state.Running = true
	job.state.LastRun = now
	job.state.LastError = ""
	runningState := job.state
	job.mu.Unlock()
	s.publish(runningState)

	err := run(ctx)
	finishedState := job.update(func(state *JobState) {
		state.Running = false
		if err != nil {
			state.LastError = err.Error()
		}
	})
	s.publish(finishedState)
	return err
}

func (j *scheduledJob) update(update func(*JobState)) JobState {
	j.mu.Lock()
	update(&j.state)
	state := j.state
	j.mu.Unlock()
	return state
}

func (s *Scheduler) publish(state JobState) {
	if s.onState != nil {
		s.onState(state)
	}
}

func nextDailyRun(now time.Time, values []ClockTime) time.Time {
	location := now.Location()
	for _, value := range values {
		candidate := time.Date(now.Year(), now.Month(), now.Day(), value.Hour, value.Minute, 0, 0, location)
		if candidate.After(now) {
			return candidate
		}
	}
	first := values[0]
	tomorrow := now.AddDate(0, 0, 1)
	return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), first.Hour, first.Minute, 0, 0, location)
}
