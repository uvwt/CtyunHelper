package automation

import (
	"fmt"
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

func (s *SafetyState) Claim(now time.Time, policy Policy, action Action) error {
	if now.Before(s.BlockedUntil) {
		return fmt.Errorf("保守策略冷却至 %s", s.BlockedUntil.Format(time.RFC3339))
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
	if s.ConsecutiveFailures >= policy.MaxFailures {
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
