package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uvwt/CtyunHelper/internal/ctyun/auth"
)

type blockingSession struct {
	starts atomic.Int32
}

func (s *blockingSession) Run(ctx context.Context) error {
	s.starts.Add(1)
	<-ctx.Done()
	return ctx.Err()
}

func TestRuntimeStartsOnlyOneSessionForRestoredBoundProfile(t *testing.T) {
	profile := auth.Profile{UserID: 1, SecretKey: "test", CommonLoginReqHeader: "common", BondedDevice: true}
	store := &memoryAccountStore{profile: profile, profileExists: true}
	client := auth.NewClient(auth.DeviceIdentity{Code: "ctyun_fixed"}, auth.ClientOptions{})
	model := NewModel(State{})
	flow := NewAuthFlow(client, store, model)
	if restored, err := flow.Restore("account"); err != nil || !restored {
		t.Fatalf("restore=%v err=%v", restored, err)
	}
	session := &blockingSession{}
	runtime := NewRuntime(model, flow, session)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime.Start(ctx)
	runtime.StartSession()
	runtime.StartSession()
	deadline := time.Now().Add(time.Second)
	for session.starts.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := session.starts.Load(); got != 1 {
		t.Fatalf("session starts = %d, want 1", got)
	}
	runtime.Stop()
}

func TestRuntimeDoesNotStartUnboundSession(t *testing.T) {
	profile := auth.Profile{UserID: 1, SecretKey: "test", CommonLoginReqHeader: "common", BondedDevice: false}
	store := &memoryAccountStore{profile: profile, profileExists: true}
	client := auth.NewClient(auth.DeviceIdentity{Code: "ctyun_fixed"}, auth.ClientOptions{})
	client.UseProfile(profile)
	model := NewModel(State{Connection: ConnectionDeviceBind})
	flow := NewAuthFlow(client, store, model)
	session := &blockingSession{}
	runtime := NewRuntime(model, flow, session)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime.Start(ctx)
	time.Sleep(20 * time.Millisecond)
	if got := session.starts.Load(); got != 0 {
		t.Fatalf("unbound session starts = %d", got)
	}
	runtime.Stop()
}
