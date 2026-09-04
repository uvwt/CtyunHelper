package automation

import (
	"fmt"
	"sync"
	"time"
)

type Action string

const (
	ActionAI     Action = "ai_chat"
	ActionLogin  Action = "login"
	ActionRedeem Action = "redeem"
)

type Policy struct {
	MaxAI           int
	MaxLogin        int
	MaxRedeem       int
	MaxFailures     int
	FailureCooldown time.Duration
}

func DefaultPolicy() Policy {
	return Policy{
		MaxAI:           2,
		MaxLogin:        2,
		MaxRedeem:       1,
		MaxFailures:     3,
		FailureCooldown: 6 * time.Hour,
	}
}

type SafetyState struct {
	Date                string         `json:"date"`
	DailyActions        map[Action]int `json:"dailyActions"`
	ConsecutiveFailures int            `json:"consecutiveFailures"`
	BlockedUntil        time.Time      `json:"blockedUntil"`
}

type GuardOptions struct {
	Now  func() time.Time
	Save func(SafetyState) error
}

// Guard 是进程内共享的保守策略入口。登录、AI、兑换通过同一个 Guard
// 占用额度；当配置 Save 时，高价值操作额度必须先成功持久化才能继续。
type Guard struct {
	mu     sync.Mutex
	policy Policy
	state  SafetyState
	now    func() time.Time
	save   func(SafetyState) error
}

func NewGuard(policy Policy, initial SafetyState, options GuardOptions) *Guard {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Guard{
		policy: policy,
		state:  cloneSafetyState(initial),
		now:    now,
		save:   options.Save,
	}
}

func (g *Guard) Claim(action Action) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	before := cloneSafetyState(g.state)
	if err := g.state.Claim(g.now(), g.policy, action); err != nil {
		return err
	}
	if err := g.persistLocked(); err != nil {
		g.state = before
		return fmt.Errorf("保存保守策略状态: %w", err)
	}
	return nil
}

func (g *Guard) RecordSuccess() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	before := cloneSafetyState(g.state)
	g.state.RecordSuccess()
	if err := g.persistLocked(); err != nil {
		g.state = before
		return fmt.Errorf("保存保守策略状态: %w", err)
	}
	return nil
}

func (g *Guard) RecordFailure() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	before := cloneSafetyState(g.state)
	g.state.RecordFailure(g.now(), g.policy)
	if err := g.persistLocked(); err != nil {
		g.state = before
		return fmt.Errorf("保存保守策略状态: %w", err)
	}
	return nil
}

func (g *Guard) Snapshot() SafetyState {
	g.mu.Lock()
	defer g.mu.Unlock()
	return cloneSafetyState(g.state)
}

func (g *Guard) persistLocked() error {
	if g.save == nil {
		return nil
	}
	return g.save(cloneSafetyState(g.state))
}

func cloneSafetyState(state SafetyState) SafetyState {
	result := state
	if state.DailyActions != nil {
		result.DailyActions = make(map[Action]int, len(state.DailyActions))
		for action, count := range state.DailyActions {
			result.DailyActions[action] = count
		}
	}
	return result
}

func (s *SafetyState) Claim(now time.Time, policy Policy, action Action) error {
	if now.Before(s.BlockedUntil) {
		return fmt.Errorf("保守策略冷却至 %s", s.BlockedUntil.Format(time.RFC3339))
	}
	if !s.BlockedUntil.IsZero() {
		// 冷却窗口完整结束后重新统计“连续失败”。否则历史失败数会让下一次
		// 单次失败立即再次进入 6 小时冷却，不符合连续失败的语义。
		s.ConsecutiveFailures = 0
		s.BlockedUntil = time.Time{}
	}
	s.resetDay(now)
	limit := policy.limit(action)
	if limit <= 0 {
		return nil
	}
	used := s.DailyActions[action]
	if used >= limit {
		return fmt.Errorf("%s 今日已达到上限 %d 次", action, limit)
	}
	s.DailyActions[action] = used + 1
	return nil
}

func (s *SafetyState) RecordSuccess() {
	s.ConsecutiveFailures = 0
	s.BlockedUntil = time.Time{}
}

func (s *SafetyState) RecordFailure(now time.Time, policy Policy) {
	s.ConsecutiveFailures++
	if policy.MaxFailures > 0 && s.ConsecutiveFailures >= policy.MaxFailures {
		s.BlockedUntil = now.Add(policy.FailureCooldown)
	}
}

func (s *SafetyState) resetDay(now time.Time) {
	day := now.Format("2006-01-02")
	if s.Date == day && s.DailyActions != nil {
		return
	}
	s.Date = day
	s.DailyActions = make(map[Action]int)
}

func (p Policy) limit(action Action) int {
	switch action {
	case ActionAI:
		return p.MaxAI
	case ActionLogin:
		return p.MaxLogin
	case ActionRedeem:
		return p.MaxRedeem
	default:
		return 0
	}
}
