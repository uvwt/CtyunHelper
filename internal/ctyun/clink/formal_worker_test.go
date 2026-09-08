package clink

import (
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/uvwt/CtyunHelper/internal/ctyun/desktop"
)

func TestFormalWorkerLogsInAttachesAndHeartbeats(t *testing.T) {
	upgrader := websocket.Upgrader{
		Subprotocols: []string{"binary"},
		CheckOrigin: func(r *http.Request) bool {
			return r.Header.Get("Origin") == defaultOrigin
		},
	}
	completed := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer ws.Close()

		if messageType, _, err := ws.ReadMessage(); err != nil || messageType != websocket.TextMessage {
			t.Errorf("proxy handshake type=%d err=%v", messageType, err)
			return
		}
		if messageType, initial, err := ws.ReadMessage(); err != nil || messageType != websocket.BinaryMessage || !IsREDQ(initial) {
			t.Errorf("initial type=%d err=%v", messageType, err)
			return
		}

		if err := ws.WriteMessage(websocket.BinaryMessage, syntheticREDQChallenge()); err != nil {
			t.Errorf("write REDQ: %v", err)
			return
		}
		if _, response, err := ws.ReadMessage(); err != nil || len(response) != 132 {
			t.Errorf("REDQ response len=%d err=%v", len(response), err)
			return
		}

		if err := ws.WriteMessage(websocket.BinaryMessage, (Message{Type: 103}).Marshal(false)); err != nil {
			t.Errorf("write 103: %v", err)
			return
		}
		_, userInfo, err := ws.ReadMessage()
		if err != nil {
			t.Errorf("read 118: %v", err)
			return
		}
		userMessages, err := ParseMessages(userInfo)
		if err != nil || len(userMessages) != 1 || userMessages[0].Type != 118 {
			t.Errorf("118=%#v err=%v", userMessages, err)
			return
		}

		_, login, err := ws.ReadMessage()
		if err != nil {
			t.Errorf("read 112: %v", err)
			return
		}
		loginMessages, err := ParseMessages(login)
		if err != nil || len(loginMessages) != 1 || loginMessages[0].Type != msgMainClientLogin {
			t.Errorf("112=%#v err=%v", loginMessages, err)
			return
		}

		loginResult := make([]byte, 4)
		binary.LittleEndian.PutUint32(loginResult, 0)
		if err := ws.WriteMessage(websocket.BinaryMessage, (Message{Type: msgMainLoginResponse, Data: loginResult}).Marshal(false)); err != nil {
			t.Errorf("write 136: %v", err)
			return
		}
		for _, want := range []uint16{msgMainAttach, msgMainClientVersion, msgHeartbeat} {
			_, raw, err := ws.ReadMessage()
			if err != nil {
				t.Errorf("read type %d: %v", want, err)
				return
			}
			messages, err := ParseMessages(raw)
			if err != nil || len(messages) != 1 || messages[0].Type != want {
				t.Errorf("want type %d, messages=%#v err=%v", want, messages, err)
				return
			}
		}
		close(completed)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	host := strings.TrimPrefix(wsURL, "ws://")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker := NewWorker(WorkerConfig{
		Connection: desktop.ConnectionInfo{
			DesktopID:           7,
			Host:                "desktop.internal",
			Port:                "443",
			ClinkLVSOutHost:     host,
			CACert:              "ca",
			ClientCert:          "client-cert",
			ClientKey:           "client-key",
			Token:               "token",
			TenantMemberAccount: "account",
		},
		UserID:            123,
		UserName:          "tester",
		DeviceCode:        "device",
		Mode:              SessionModeFormal,
		ReconnectInterval: 10 * time.Second,
		HeartbeatInterval: 20 * time.Millisecond,
		ErrorBackoff:      10 * time.Millisecond,
	}, nil)
	worker.dialer = &websocket.Dialer{HandshakeTimeout: time.Second, Subprotocols: []string{"binary"}}

	if err := worker.session.Transition(StateResolving, nil); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- worker.runFormalCycleWithURL(ctx, wsURL+"/clinkProxy/7/MAIN") }()
	select {
	case <-completed:
		cancel()
	case err := <-done:
		t.Fatalf("formal worker stopped early: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("formal clink test timed out")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("formal worker did not stop")
	}
	snapshot := worker.Snapshot()
	if snapshot.REDQChallenges != 1 || snapshot.REDQResponses != 1 || snapshot.UserInfoRequests != 1 || snapshot.UserInfoResponses != 1 || snapshot.ClientLogins != 1 || snapshot.LoginResponses != 1 || snapshot.LastLoginResult != 0 || snapshot.AttachRequests != 1 || snapshot.Heartbeats < 1 {
		t.Fatalf("formal counters=%#v", snapshot)
	}
}
