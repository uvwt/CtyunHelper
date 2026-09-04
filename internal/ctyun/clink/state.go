package clink

import "fmt"

type State string

const (
	StateIdle         State = "idle"
	StateResolving    State = "resolving"
	StateConnecting   State = "connecting"
	StateHandshaking  State = "handshaking"
	StateOnline       State = "online"
	StateBackoff      State = "backoff"
	StateAuthRequired State = "auth_required"
	StatePaused       State = "paused"
	StateStopped      State = "stopped"
	StateFatal        State = "fatal"
)

var transitions = map[State]map[State]struct{}{
	StateIdle: {
		StateResolving: {}, StateStopped: {},
	},
	StateResolving: {
		StateConnecting: {}, StateAuthRequired: {}, StateBackoff: {}, StateFatal: {}, StateStopped: {},
	},
	StateConnecting: {
		StateHandshaking: {}, StateBackoff: {}, StateAuthRequired: {}, StateFatal: {}, StateStopped: {},
	},
	StateHandshaking: {
		StateOnline: {}, StateBackoff: {}, StateAuthRequired: {}, StateFatal: {}, StateStopped: {},
	},
	StateOnline: {
		StateBackoff: {}, StatePaused: {}, StateAuthRequired: {}, StateFatal: {}, StateStopped: {},
	},
	StateBackoff: {
		StateResolving: {}, StatePaused: {}, StateAuthRequired: {}, StateFatal: {}, StateStopped: {},
	},
	StateAuthRequired: {
		StateResolving: {}, StateStopped: {},
	},
	StatePaused: {
		StateResolving: {}, StateStopped: {},
	},
	StateFatal: {
		StateStopped: {},
	},
	StateStopped: {},
}

func CanTransition(from, to State) bool {
	_, ok := transitions[from][to]
	return ok
}

func ValidateTransition(from, to State) error {
	if from == to {
		return nil
	}
	if !CanTransition(from, to) {
		return fmt.Errorf("clink: 非法状态转换 %s -> %s", from, to)
	}
	return nil
}
