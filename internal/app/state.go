package app

import (
	"sync"
	"time"
)

type ConnectionState string

const (
	ConnectionStopped    ConnectionState = "stopped"
	ConnectionConnecting ConnectionState = "connecting"
	ConnectionOnline     ConnectionState = "online"
	ConnectionBackoff    ConnectionState = "backoff"
	ConnectionPaused     ConnectionState = "paused"
	ConnectionAuth       ConnectionState = "auth_required"
	ConnectionDeviceBind ConnectionState = "device_binding_required"
	ConnectionError      ConnectionState = "error"
)

type JobStatus struct {
	Running   bool
	LastRun   time.Time
	NextRun   time.Time
	LastError string
}

type UsageTaskStatus struct {
	Found    bool
	Status   int
	Progress int
}

type State struct {
	Account          string
	DesktopID        string
	DesktopName      string
	Connection       ConnectionState
	OnlineSince      time.Time
	Points           int
	UsageTask        UsageTaskStatus
	AITask           JobStatus
	PointsTask       JobStatus
	RedeemTask       JobStatus
	RedeemEnabled    bool
	RedeemSummary    string
	AutomationPaused bool
	LastError        string
	ChangedAt        time.Time
}

// Model 是进程内唯一的 UI 可观察状态。协议层只通过 App 更新 Model，
// Windows 窗口不直接持有 Auth/Desktop/Clink Client。
type Model struct {
	mu     sync.RWMutex
	state  State
	events *Events
}

func NewModel(initial State) *Model {
	if initial.Connection == "" {
		initial.Connection = ConnectionStopped
	}
	if initial.ChangedAt.IsZero() {
		initial.ChangedAt = time.Now()
	}
	return &Model{state: initial, events: NewEvents()}
}

func (m *Model) Snapshot() State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

func (m *Model) Events() *Events {
	return m.events
}

func (m *Model) Update(update func(*State)) State {
	m.mu.Lock()
	update(&m.state)
	m.state.ChangedAt = time.Now()
	snapshot := m.state
	m.mu.Unlock()
	m.events.Publish(Event{Type: EventStateChanged, Data: snapshot})
	return snapshot
}
