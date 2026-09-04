package clink

import (
	"sync"
	"time"
)

type Snapshot struct {
	State       State
	ChangedAt   time.Time
	OnlineSince time.Time
	LastError   string
}

type Session struct {
	mu       sync.RWMutex
	snapshot Snapshot
	notify   func(Snapshot)
}

func NewSession(notify func(Snapshot)) *Session {
	return &Session{
		snapshot: Snapshot{State: StateIdle, ChangedAt: time.Now()},
		notify:   notify,
	}
}

func (s *Session) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot
}

func (s *Session) Transition(next State, err error) error {
	s.mu.Lock()
	if transitionErr := ValidateTransition(s.snapshot.State, next); transitionErr != nil {
		s.mu.Unlock()
		return transitionErr
	}

	now := time.Now()
	s.snapshot.State = next
	s.snapshot.ChangedAt = now
	if err != nil {
		s.snapshot.LastError = err.Error()
	} else if next != StateBackoff && next != StateFatal && next != StateAuthRequired {
		s.snapshot.LastError = ""
	}
	if next == StateOnline && s.snapshot.OnlineSince.IsZero() {
		s.snapshot.OnlineSince = now
	}
	if next == StateStopped {
		s.snapshot.OnlineSince = time.Time{}
	}
	snapshot := s.snapshot
	s.mu.Unlock()

	if s.notify != nil {
		s.notify(snapshot)
	}
	return nil
}
