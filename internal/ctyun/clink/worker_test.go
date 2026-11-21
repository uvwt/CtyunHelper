package clink

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
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
	go func() {
		_, err := worker.runCycleWithURL(ctx, wsURL+"/clinkProxy/7/MAIN")
		done <- err
	}()
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
	snapshot := worker.Snapshot()
	if snapshot.REDQChallenges != 1 || snapshot.REDQResponses != 1 || snapshot.UserInfoRequests != 1 || snapshot.UserInfoResponses != 1 {
		t.Fatalf("protocol counters = %#v", snapshot)
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

func TestWorkerRequestsRouteRefreshAfterConsecutiveRapidCloses(t *testing.T) {
	upgrader := websocket.Upgrader{
		Subprotocols: []string{"binary"},
		CheckOrigin:  func(*http.Request) bool { return true },
	}
	var attempts atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		attempts.Add(1)
		// 模拟路由/证书失效后的典型现象：收到握手和初始帧前或刚收到后立即异常断开（close 1006），
		// 全程不返回有效 REDQ 校验或 103 请求。
		_, _, _ = ws.ReadMessage()
		_ = ws.Close()
	}))
	defer server.Close()

	host := strings.TrimPrefix(server.URL, "https://")
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
		ReconnectInterval: 5 * time.Second,
		ErrorBackoff:      10 * time.Millisecond,
	}, nil)
	worker.dialer = &websocket.Dialer{
		HandshakeTimeout: time.Second,
		Subprotocols:     []string{"binary"},
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := worker.Run(ctx)
	if !errors.Is(err, ErrRouteRefreshRequired) {
		t.Fatalf("Run() error = %v, want ErrRouteRefreshRequired", err)
	}
	if got := attempts.Load(); got != defaultMaxConsecutiveFailures {
		t.Fatalf("attempts = %d, want %d", got, defaultMaxConsecutiveFailures)
	}
	if worker.HadHealthyCycle() {
		t.Fatal("HadHealthyCycle() = true, want false")
	}
}

func TestWorkerResetsFailuresAfterHealthyCycleAndDetectsClockJump(t *testing.T) {
	upgrader := websocket.Upgrader{
		Subprotocols: []string{"binary"},
		CheckOrigin:  func(*http.Request) bool { return true },
	}
	var attempts atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		n := attempts.Add(1)
		if _, _, err := ws.ReadMessage(); err != nil {
			return
		}
		if _, _, err := ws.ReadMessage(); err != nil {
			return
		}
		if n == 1 {
			// 第 1 轮先模拟一次瞬断，尚未达到刷新阈值。
			return
		}
		// 第 2 轮完成有效 REDQ 校验并保持到周期结束，应重置连续失败计数并记录健康周期。
		_ = ws.WriteMessage(websocket.BinaryMessage, syntheticREDQChallenge())
		_, _, _ = ws.ReadMessage()
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	host := strings.TrimPrefix(server.URL, "https://")
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
		ReconnectInterval: 150 * time.Millisecond,
		ErrorBackoff:      10 * time.Millisecond,
	}, nil)
	worker.dialer = &websocket.Dialer{
		HandshakeTimeout: time.Second,
		Subprotocols:     []string{"binary"},
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true},
	}

	baseWall := time.Date(2026, 11, 21, 21, 0, 0, 0, time.Local)
	var clockMu sync.Mutex
	clockOffset := time.Duration(0)
	worker.now = func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		// 第 3 轮连接开始后注入显著墙钟跳变，模拟系统休眠唤醒。
		if attempts.Load() >= 3 && clockOffset == 0 {
			clockOffset = 2 * time.Minute
		}
		return baseWall.Add(clockOffset)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := worker.Run(ctx)
	if !errors.Is(err, ErrRouteRefreshRequired) {
		t.Fatalf("Run() error = %v, want ErrRouteRefreshRequired", err)
	}
	if !worker.HadHealthyCycle() {
		t.Fatal("HadHealthyCycle() = false, want true")
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

func TestDetectClockJump(t *testing.T) {
	start := time.Date(2026, 11, 21, 21, 0, 0, 0, time.Local)
	if detectClockJump(start, start.Add(10*time.Second), 60*time.Second, 30*time.Second) {
		t.Fatal("normal elapsed time should not trigger clock jump")
	}
	if !detectClockJump(start, start.Add(100*time.Second), 60*time.Second, 30*time.Second) {
		t.Fatal("wall elapsed far exceeding expected duration should trigger clock jump")
	}
}
