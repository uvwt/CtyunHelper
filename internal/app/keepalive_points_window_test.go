package app

import (
	"testing"
	"time"

	"github.com/uvwt/CtyunHelper/internal/ctyun/clink"
)

func TestKeepaliveSessionModeFollowsUsagePointsWindow(t *testing.T) {
	policy, err := NewPointsSessionPolicy(UsagePointsWindow{Enabled: true, Start: "04:00", End: "07:00"})
	if err != nil {
		t.Fatal(err)
	}
	keepalive := &Keepalive{pointsPolicy: policy}
	formalMode, formalReconnect := keepalive.sessionMode(mustTime(t, "2026-09-08 05:00"))
	if formalMode != clink.SessionModeFormal || formalReconnect != 80*time.Minute {
		t.Fatalf("formal mode=%v reconnect=%v", formalMode, formalReconnect)
	}
	lightMode, lightReconnect := keepalive.sessionMode(mustTime(t, "2026-09-08 08:00"))
	if lightMode != clink.SessionModeKeepalive || lightReconnect != 60*time.Second {
		t.Fatalf("light mode=%v reconnect=%v", lightMode, lightReconnect)
	}
}

func TestKeepaliveSessionModeDefaultsToLightweight(t *testing.T) {
	keepalive := &Keepalive{}
	mode, reconnect := keepalive.sessionMode(time.Now())
	if mode != clink.SessionModeKeepalive || reconnect != 60*time.Second {
		t.Fatalf("mode=%v reconnect=%v", mode, reconnect)
	}
}
