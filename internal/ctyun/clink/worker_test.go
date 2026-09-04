package clink

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/uvwt/CtyunHelper/internal/ctyun/desktop"
)

func TestWorkerCompletesClinkHandshakeAndResponses(t *testing.T) {
	upgrader := websocket.Upgrader{
		Subprotocols: []string{"binary"},
		CheckOrigin: func(r *http.Request) bool {
			return r.Header.Get("Origin") == defaultOrigin
		},
	}
	completed := make(chan struct{})
	var once sync.Once

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer ws.Close()

		messageType, rawHandshake, err := ws.ReadMessage()
		if err != nil || messageType != websocket.TextMessage {
			t.Errorf("read handshake: type=%d err=%v", messageType, err)
			return
		}
		var handshake ProxyHandshake
		if err := json.Unmarshal(rawHandshake, &handshake); err != nil {
			t.Errorf("decode handshake: %v", err)
			return
		}
		if handshake.ServerName != "desktop.internal:443" || handshake.Cert != "client-cert" {
			t.Errorf("handshake=%#v", handshake)
			return
		}

		messageType, initial, err := ws.ReadMessage()
		if err != nil || messageType != websocket.BinaryMessage || !IsREDQ(initial) {
			t.Errorf("initial: type=%d err=%v data=%x", messageType, err, initial)
			return
		}

		challenge := syntheticREDQChallenge()
		if err := ws.WriteMessage(websocket.BinaryMessage, challenge); err != nil {
			t.Errorf("write REDQ: %v", err)
			return
		}
		messageType, response, err := ws.ReadMessage()
		if err != nil || messageType != websocket.BinaryMessage || len(response) != 132 || response[0] != 1 {
			t.Errorf("REDQ response: type=%d len=%d err=%v", messageType, len(response), err)
			return
		}

		// 第三方 CtYun 对普通 SendInfo 解析失败只记录后继续收包。先发送一个
		// 声明超长 payload 的残缺非 REDQ 帧；Worker 不能因此断开，否则后续
		// 合法 103 请求将收不到 118 响应。
		malformed := []byte{42, 0, 0xff, 0xff, 0xff, 0x7f, 1, 2, 3}
		if err := ws.WriteMessage(websocket.BinaryMessage, malformed); err != nil {
			t.Errorf("write malformed frame: %v", err)
			return
		}

		requestUser := Message{Type: 103, Data: []byte("request")}.Marshal(false)
		requestUser = append(requestUser, 1, 2, 3) // 合法 103 后附损坏尾部，前缀仍必须处理。
		if err := ws.WriteMessage(websocket.BinaryMessage, requestUser); err != nil {
			t.Errorf("write type103: %v", err)
			return
		}
		messageType, userResponse, err := ws.ReadMessage()
		if err != nil || messageType != websocket.BinaryMessage {
			t.Errorf("read type118: %v", err)
			return
		}
		messages, err := ParseMessages(userResponse)
		if err != nil || len(messages) != 1 || messages[0].Type != 118 || !strings.Contains(string(messages[0].Data), `"userName":"tester"`) {
			t.Errorf("type118 messages=%#v err=%v", messages, err)
			return
		}
		once.Do(func() { close(completed) })
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	host := strings.TrimPrefix(wsURL, "ws://")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker := NewWorker(WorkerConfig{
		Connection: desktop.ConnectionInfo{
			DesktopID:       7,
			Host:            "desktop.internal",
			Port:            "443",
			ClinkLVSOutHost: host,
			CACert:          "ca",
			ClientCert:      "client-cert",
			ClientKey:       "client-key",
		},
		UserID:            123,
		UserName:          "tester",
		ReconnectInterval: 10 * time.Second,
		ErrorBackoff:      10 * time.Millisecond,
	}, nil)
	// httptest 是 ws://，测试中覆盖 Dialer 的 URL scheme；生产仍固定 wss://。
	worker.dialer = &websocket.Dialer{HandshakeTimeout: time.Second, Subprotocols: []string{"binary"}}

	if err := worker.session.Transition(StateResolving, nil); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- worker.runCycleWithURL(ctx, wsURL+"/clinkProxy/7/MAIN") }()
	select {
	case <-completed:
		cancel()
	case <-time.After(3 * time.Second):
		t.Fatal("clink test timed out")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop")
	}
}

func syntheticREDQChallenge() []byte {
	challenge := make([]byte, 182)
	copy(challenge, []byte("REDQ"))
	for i := 49; i < 177; i++ {
		challenge[i] = 0xff
	}
	challenge[176] = 0xc5
	challenge[179], challenge[180], challenge[181] = 1, 0, 1
	return challenge
}
