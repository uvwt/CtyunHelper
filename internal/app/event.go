package app

import "sync"

type EventType string

const (
	EventStateChanged EventType = "state_changed"
	EventLogAdded     EventType = "log_added"
)

type Event struct {
	Type EventType
	Data any
}

type Events struct {
	mu   sync.RWMutex
	next uint64
	subs map[uint64]chan Event
}

func NewEvents() *Events {
	return &Events{subs: make(map[uint64]chan Event)}
}

func (e *Events) Subscribe(buffer int) (<-chan Event, func()) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.next++
	id := e.next
	ch := make(chan Event, buffer)
	e.subs[id] = ch
	return ch, func() {
		e.mu.Lock()
		defer e.mu.Unlock()
		if current, ok := e.subs[id]; ok {
			delete(e.subs, id)
			close(current)
		}
	}
}

func (e *Events) Publish(event Event) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, ch := range e.subs {
		select {
		case ch <- event:
		default:
		}
	}
}
