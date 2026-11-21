package clink

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	TransportDirect         = "direct"
	TransportProxy          = "proxy"
	TransportDirectFallback = "direct_fallback"

	defaultProxyHandshakeTimeout = 8 * time.Second
	defaultDirectFallbackTTL     = 10 * time.Minute
)

func parseProxyServer(value, targetScheme string) (*url.URL, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}

	selected := value
	if strings.Contains(value, "=") {
		selected = ""
		want := "http"
		if targetScheme == "https" || targetScheme == "wss" {
			want = "https"
		}
		var fallback string
		for _, item := range strings.Split(value, ";") {
			name, raw, ok := strings.Cut(strings.TrimSpace(item), "=")
			if !ok {
				continue
			}
			name = strings.ToLower(strings.TrimSpace(name))
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			if fallback == "" && (name == "http" || name == "https") {
				fallback = raw
			}
			if name == want {
				selected = raw
				break
			}
		}
		if selected == "" {
			selected = fallback
		}
	}
	if selected == "" {
		return nil, nil
	}
	if !strings.Contains(selected, "://") {
		selected = "http://" + selected
	}
	return url.Parse(selected)
}

func environmentProxy(req *http.Request) (*url.URL, error) {
	return http.ProxyFromEnvironment(req)
}

// transportPolicy 负责 Clink WebSocket 的代理与直连健康切换。
// 默认保持系统代理兼容；当本地代理拒绝连接、握手超时或建连后出现读取超时时，
// 立即回退直连并进入一段冷却期，避免异常代理反复拖垮保活周期。
type transportPolicy struct {
	mu                    sync.Mutex
	directFallbackUntil   time.Time
	directFallbackTTL     time.Duration
	proxyHandshakeTimeout time.Duration
	now                   func() time.Time
}

func newTransportPolicy(now func() time.Time) *transportPolicy {
	if now == nil {
		now = time.Now
	}
	return &transportPolicy{
		directFallbackTTL:     defaultDirectFallbackTTL,
		proxyHandshakeTimeout: defaultProxyHandshakeTimeout,
		now:                   now,
	}
}

func (p *transportPolicy) preferDirect() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.directFallbackUntil.IsZero() && p.now().Before(p.directFallbackUntil)
}

func (p *transportPolicy) markProxyFailed() {
	p.mu.Lock()
	p.directFallbackUntil = p.now().Add(p.directFallbackTTL)
	p.mu.Unlock()
}

func (p *transportPolicy) markProxyHealthy() {
	p.mu.Lock()
	p.directFallbackUntil = time.Time{}
	p.mu.Unlock()
}

func (p *transportPolicy) dial(ctx context.Context, base *websocket.Dialer, endpoint string, headers http.Header) (*websocket.Conn, *http.Response, string, error) {
	if base == nil {
		return nil, nil, TransportDirect, fmt.Errorf("clink: WebSocket dialer 未初始化")
	}
	if base.Proxy == nil {
		ws, resp, err := base.DialContext(ctx, endpoint, headers)
		return ws, resp, TransportDirect, err
	}

	proxyReq, err := proxyRequestForEndpoint(endpoint)
	if err != nil {
		ws, resp, dialErr := dialWithoutProxy(ctx, base, endpoint, headers)
		return ws, resp, TransportDirect, dialErr
	}
	proxyURL, proxyErr := base.Proxy(proxyReq)
	if proxyErr == nil && proxyURL == nil {
		ws, resp, dialErr := base.DialContext(ctx, endpoint, headers)
		return ws, resp, TransportDirect, dialErr
	}
	if proxyErr != nil {
		// 代理配置本身不可解析时直接回退直连，不阻断保活建连。
		ws, resp, dialErr := dialWithoutProxy(ctx, base, endpoint, headers)
		return ws, resp, TransportDirectFallback, dialErr
	}

	dialViaProxy := func() (*websocket.Conn, *http.Response, error) {
		dialer := *base
		dialer.Proxy = http.ProxyURL(proxyURL)
		if p.proxyHandshakeTimeout > 0 && (dialer.HandshakeTimeout <= 0 || dialer.HandshakeTimeout > p.proxyHandshakeTimeout) {
			// 有直连兜底时收敛代理握手超时，防止挂死代理长时间占满周期。
			dialer.HandshakeTimeout = p.proxyHandshakeTimeout
		}
		return dialer.DialContext(ctx, endpoint, headers)
	}

	if p.preferDirect() {
		ws, resp, directErr := dialWithoutProxy(ctx, base, endpoint, headers)
		if directErr == nil {
			return ws, resp, TransportDirectFallback, nil
		}
		if ctx.Err() != nil {
			return nil, resp, TransportDirectFallback, directErr
		}
		// 直连在冷却期内不可达时（例如切入必须走代理的网络）立即回探代理。
		ws, proxyResp, proxyDialErr := dialViaProxy()
		if proxyDialErr == nil {
			p.markProxyHealthy()
			return ws, proxyResp, TransportProxy, nil
		}
		return nil, proxyResp, TransportProxy, fmt.Errorf("直连失败(%v)，回退代理(%s)失败: %w", directErr, proxyURL.Host, proxyDialErr)
	}

	ws, resp, proxyDialErr := dialViaProxy()
	if proxyDialErr == nil {
		p.markProxyHealthy()
		return ws, resp, TransportProxy, nil
	}
	if ctx.Err() != nil {
		return nil, resp, TransportProxy, proxyDialErr
	}

	ws, directResp, directErr := dialWithoutProxy(ctx, base, endpoint, headers)
	if directErr == nil {
		p.markProxyFailed()
		return ws, directResp, TransportDirectFallback, nil
	}
	return nil, directResp, TransportDirectFallback, fmt.Errorf("经代理(%s)连接失败(%v)，回退直连失败: %w", proxyURL.Host, proxyDialErr, directErr)
}

func dialWithoutProxy(ctx context.Context, base *websocket.Dialer, endpoint string, headers http.Header) (*websocket.Conn, *http.Response, error) {
	dialer := *base
	dialer.Proxy = nil
	return dialer.DialContext(ctx, endpoint, headers)
}

func proxyRequestForEndpoint(endpoint string) (*http.Request, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	proxyURL := *parsed
	switch strings.ToLower(proxyURL.Scheme) {
	case "wss":
		proxyURL.Scheme = "https"
	case "ws":
		proxyURL.Scheme = "http"
	}
	return &http.Request{URL: &proxyURL}, nil
}

func isProxyTransportTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
