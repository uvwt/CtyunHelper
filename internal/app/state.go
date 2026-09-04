package app

import "time"

type ConnectionState string

const (
	ConnectionStopped    ConnectionState = "stopped"
	ConnectionConnecting ConnectionState = "connecting"
	ConnectionOnline     ConnectionState = "online"
	ConnectionBackoff    ConnectionState = "backoff"
)

type State struct {
	Account          string
	Connection       ConnectionState
	OnlineSince      time.Time
	Points           int
	AutomationPaused bool
	LastError        string
}
