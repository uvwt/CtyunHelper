package clink

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestParseProxyServer(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		scheme string
		want   string
	}{
		{name: "generic", value: "127.0.0.1:10808", scheme: "wss", want: "http://127.0.0.1:10808"},
		{name: "https mapping", value: "http=127.0.0.1:8080;https=127.0.0.1:8443", scheme: "wss", want: "http://127.0.0.1:8443"},
		{name: "preserve scheme", value: "socks5://127.0.0.1:1080", scheme: "wss", want: "socks5://127.0.0.1:1080"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseProxyServer(tt.value, tt.scheme)
			if err != nil {
				t.Fatal(err)
			}
			if got == nil || got.String() != tt.want {
				t.Fatalf("proxy=%v, want %s", got, tt.want)
			}
		})
	}
}

func TestTransportPolicyFallsBackToDirectWhenProxyRefusesOrHangs(t *testing.T) {
	upgrader := websocket.Upgrader{
		Subprotocols: []string{"binary"},
		CheckOrigin:  func(*http.Request) bool { return true },
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer target.Close()
	wsEndpoint := "ws" + strings.TrimPrefix(target.URL, "http") + "/clinkProxy/7/MAIN"

	t.Run("proxy hangs then falls back to direct and stays on cooldown", func(t *testing.T) {
		hangingListener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer hangingListener.Close()

		var proxyHits atomic.Int32
		stopHang := make(chan struct{})
		defer close(stopHang)
		go func() {
			for {
				conn, err := hangingListener.Accept()
				if err != nil {
					return
				}
				proxyHits.Add(1)
				go func(c net.Conn) {
					defer c.Close()
					<-stopHang
				}(conn)
			}
		}()

		proxyURL, err := url.Parse("http://" + hangingListener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		currentTime := time.Date(2026, 11, 21, 17, 30, 0, 0, time.Local)
		policy := newTransportPolicy(func() time.Time { return currentTime })
		policy.proxyHandshakeTimeout = 150 * time.Millisecond
		policy.directFallbackTTL = 5 * time.Minute

		base := &websocket.Dialer{
			HandshakeTimeout: 2 * time.Second,
			Subprotocols:     []string{"binary"},
			Proxy:            http.ProxyURL(proxyURL),
		}

		ws, _, transport, err := policy.dial(context.Background(), base, wsEndpoint, http.Header{"Origin": {defaultOrigin}})
		if err != nil {
			t.Fatalf("first dial() error = %v", err)
		}
		_ = ws.Close()
		if transport != TransportDirectFallback {
			t.Fatalf("first transport = %q, want %q", transport, TransportDirectFallback)
		}
		if got := proxyHits.Load(); got != 1 {
			t.Fatalf("proxy hits after first dial = %d, want 1", got)
		}

		// 冷却期内应直接走直连，不再浪费时间探测挂死代理。
		currentTime = currentTime.Add(1 * time.Minute)
		ws, _, transport, err = policy.dial(context.Background(), base, wsEndpoint, http.Header{"Origin": {defaultOrigin}})
		if err != nil {
			t.Fatalf("cooldown dial() error = %v", err)
		}
		_ = ws.Close()
		if transport != TransportDirectFallback {
			t.Fatalf("cooldown transport = %q, want %q", transport, TransportDirectFallback)
		}
		if got := proxyHits.Load(); got != 1 {
			t.Fatalf("proxy hits during cooldown = %d, want 1", got)
		}
	})

	t.Run("proxy recovers after cooldown expires", func(t *testing.T) {
		var proxyEnabled atomic.Bool
		var proxyHits atomic.Int32
		proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer proxyListener.Close()

		go func() {
			for {
				clientConn, err := proxyListener.Accept()
				if err != nil {
					return
				}
				proxyHits.Add(1)
				if !proxyEnabled.Load() {
					_ = clientConn.Close()
					continue
				}
				go handleTestHTTPConnectProxy(clientConn)
			}
		}()

		proxyURL, err := url.Parse("http://" + proxyListener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		var mu sync.Mutex
		currentTime := time.Date(2026, 11, 21, 18, 0, 0, 0, time.Local)
		policy := newTransportPolicy(func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			return currentTime
		})
		policy.proxyHandshakeTimeout = 200 * time.Millisecond
		policy.directFallbackTTL = 2 * time.Minute

		base := &websocket.Dialer{
			HandshakeTimeout: 2 * time.Second,
			Subprotocols:     []string{"binary"},
			Proxy:            http.ProxyURL(proxyURL),
		}

		// 代理故障时自动回退直连。
		ws, _, transport, err := policy.dial(context.Background(), base, wsEndpoint, http.Header{"Origin": {defaultOrigin}})
		if err != nil {
			t.Fatalf("fallback dial() error = %v", err)
		}
		_ = ws.Close()
		if transport != TransportDirectFallback {
			t.Fatalf("transport = %q, want %q", transport, TransportDirectFallback)
		}

		// 冷却期结束后代理恢复可用，应切回代理兼容模式。
		proxyEnabled.Store(true)
		mu.Lock()
		currentTime = currentTime.Add(3 * time.Minute)
		mu.Unlock()

		ws, _, transport, err = policy.dial(context.Background(), base, wsEndpoint, http.Header{"Origin": {defaultOrigin}})
		if err != nil {
			t.Fatalf("recovered proxy dial() error = %v", err)
		}
		_ = ws.Close()
		if transport != TransportProxy {
			t.Fatalf("recovered transport = %q, want %q", transport, TransportProxy)
		}
		if got := proxyHits.Load(); got != 2 {
			t.Fatalf("proxy hits = %d, want 2", got)
		}
	})

	t.Run("direct failure during cooldown falls back to proxy", func(t *testing.T) {
		var proxyHits atomic.Int32
		proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer proxyListener.Close()

		go func() {
			for {
				clientConn, err := proxyListener.Accept()
				if err != nil {
					return
				}
				proxyHits.Add(1)
				go handleTestHTTPConnectProxy(clientConn)
			}
		}()

		proxyURL, err := url.Parse("http://" + proxyListener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		currentTime := time.Date(2026, 11, 21, 18, 30, 0, 0, time.Local)
		policy := newTransportPolicy(func() time.Time { return currentTime })
		policy.directFallbackTTL = 5 * time.Minute
		policy.markProxyFailed() // 模拟处于直连冷却期

		// 模拟直连目标地址不可达，但经代理可转发到 target。
		targetAddr := strings.TrimPrefix(target.URL, "http://")
		base := &websocket.Dialer{
			HandshakeTimeout: 2 * time.Second,
			Subprotocols:     []string{"binary"},
			Proxy:            http.ProxyURL(proxyURL),
			NetDialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				if addr == targetAddr {
					return nil, errors.New("direct route blocked")
				}
				return (&net.Dialer{}).DialContext(ctx, network, addr)
			},
		}

		ws, _, transport, err := policy.dial(context.Background(), base, wsEndpoint, http.Header{"Origin": {defaultOrigin}})
		if err != nil {
			t.Fatalf("dial() error = %v", err)
		}
		_ = ws.Close()
		if transport != TransportProxy {
			t.Fatalf("transport = %q, want %q", transport, TransportProxy)
		}
		if policy.preferDirect() {
			t.Fatal("preferDirect() = true after proxy recovered, want false")
		}
		if got := proxyHits.Load(); got != 1 {
			t.Fatalf("proxy hits = %d, want 1", got)
		}
	})
}

func handleTestHTTPConnectProxy(clientConn net.Conn) {
	defer clientConn.Close()
	reader := bufio.NewReader(clientConn)
	req, err := http.ReadRequest(reader)
	if err != nil {
		return
	}
	targetConn, err := net.DialTimeout("tcp", req.Host, 2*time.Second)
	if err != nil {
		_, _ = io.WriteString(clientConn, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
		return
	}
	defer targetConn.Close()
	_, _ = io.WriteString(clientConn, "HTTP/1.1 200 Connection Established\r\n\r\n")
	go func() {
		_, _ = io.Copy(targetConn, reader)
		_ = targetConn.Close()
	}()
	_, _ = io.Copy(clientConn, targetConn)
}
