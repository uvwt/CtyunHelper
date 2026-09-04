package clink

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/uvwt/CtyunHelper/internal/ctyun/desktop"
)

const defaultOrigin = "https://pc.ctyun.cn"

type WorkerConfig struct {
	Connection        desktop.ConnectionInfo
	UserID            int64
	UserName          string
	ReconnectInterval time.Duration
	ErrorBackoff      time.Duration
}

type Worker struct {
	config  WorkerConfig
	session *Session
	dialer  *websocket.Dialer
}

func NewWorker(config WorkerConfig, notify func(Snapshot)) *Worker {
	if config.ReconnectInterval <= 0 {
		config.ReconnectInterval = 60 * time.Second
	}
	if config.ErrorBackoff <= 0 {
		config.ErrorBackoff = 5 * time.Second
	}
	return &Worker{
		config:  config,
		session: NewSession(notify),
		dialer: &websocket.Dialer{
			HandshakeTimeout: 15 * time.Second,
			Subprotocols:     []string{"binary"},
		},
	}
}

func (w *Worker) Snapshot() Snapshot {
	return w.session.Snapshot()
}

// Run 是 Clink 保活的长期主流程。每个连接周期复现官方代理握手所需消息，
// 服务端校验与用户信息响应都在同一个读循环中完成；UI 和调度层不接触 WebSocket。
func (w *Worker) Run(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			_ = w.session.Transition(StateStopped, nil)
			return ctx.Err()
		}

		if err := w.session.Transition(StateResolving, nil); err != nil {
			return err
		}
		err := w.runCycle(ctx)
		if ctx.Err() != nil {
			_ = w.session.Transition(StateStopped, nil)
			return ctx.Err()
		}
		if err := w.session.Transition(StateBackoff, err); err != nil {
			return err
		}
		if err == nil {
			// 第三方 CtYun 的强制周期结束后立即进入下一轮连接，不额外等待。
			// runCycle 正常返回只表示 ReconnectInterval 到期；网络/协议错误都会
			// 带 error 返回并走下面的固定错误退避，因此这里不会形成失败风暴。
			continue
		}

		timer := time.NewTimer(w.config.ErrorBackoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = w.session.Transition(StateStopped, nil)
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (w *Worker) runCycle(ctx context.Context) error {
	connection := w.config.Connection
	if connection.DesktopID == 0 || connection.ClinkLVSOutHost == "" {
		return fmt.Errorf("clink: 连接参数不完整")
	}
	endpoint := url.URL{
		Scheme: "wss",
		Host:   connection.ClinkLVSOutHost,
		Path:   fmt.Sprintf("/clinkProxy/%d/MAIN", connection.DesktopID),
	}
	return w.runCycleWithURL(ctx, endpoint.String())
}

func (w *Worker) runCycleWithURL(ctx context.Context, endpoint string) error {
	connection := w.config.Connection
	if err := w.session.Transition(StateConnecting, nil); err != nil {
		return err
	}
	headers := http.Header{"Origin": {defaultOrigin}}
	ws, response, err := w.dialer.DialContext(ctx, endpoint, headers)
	if err != nil {
		if response != nil {
			return fmt.Errorf("clink: WebSocket 连接失败 HTTP %d: %w", response.StatusCode, err)
		}
		return fmt.Errorf("clink: WebSocket 连接失败: %w", err)
	}
	defer ws.Close()

	if err := w.session.Transition(StateHandshaking, nil); err != nil {
		return err
	}
	handshake := NewProxyHandshake(
		connection.ClinkLVSOutHost,
		connection.Host,
		connection.Port,
		connection.CACert,
		connection.ClientCert,
		connection.ClientKey,
	)
	payload, err := handshake.JSON()
	if err != nil {
		return fmt.Errorf("clink: 编码代理握手: %w", err)
	}
	if err := ws.WriteMessage(websocket.TextMessage, payload); err != nil {
		return fmt.Errorf("clink: 发送代理握手: %w", err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(500 * time.Millisecond):
	}
	if err := ws.WriteMessage(websocket.BinaryMessage, InitialPayload()); err != nil {
		return fmt.Errorf("clink: 发送初始 REDQ 帧: %w", err)
	}
	if err := w.session.Transition(StateOnline, nil); err != nil {
		return err
	}

	cycleCtx, cancel := context.WithTimeout(ctx, w.config.ReconnectInterval)
	defer cancel()
	closed := make(chan struct{})
	go func() {
		select {
		case <-cycleCtx.Done():
			_ = ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "cycle reset"), time.Now().Add(time.Second))
			_ = ws.Close()
		case <-closed:
		}
	}()
	defer close(closed)

	for {
		messageType, data, err := ws.ReadMessage()
		if err != nil {
			if cycleCtx.Err() != nil {
				return nil
			}
			return fmt.Errorf("clink: 读取 WebSocket: %w", err)
		}
		if messageType != websocket.BinaryMessage || len(data) == 0 {
			continue
		}
		if IsREDQ(data) {
			response, err := BuildREDQResponse(data)
			if err != nil {
				return err
			}
			if err := ws.WriteMessage(websocket.BinaryMessage, response); err != nil {
				return fmt.Errorf("clink: 发送 REDQ 响应: %w", err)
			}
			continue
		}

		messages, parseErr := ParseMessages(data)
		for _, message := range messages {
			if message.Type != 103 {
				continue
			}
			userInfo, err := BuildUserInfoMessage(w.config.UserID, w.config.UserName)
			if err != nil {
				return fmt.Errorf("clink: 编码用户信息: %w", err)
			}
			if err := ws.WriteMessage(websocket.BinaryMessage, userInfo); err != nil {
				return fmt.Errorf("clink: 发送用户信息: %w", err)
			}
		}
		if parseErr != nil {
			// CtYun 第三方实现把普通 Clink 消息解析放在独立 try/catch 中：
			// 单个未知/残缺非 REDQ 尾部不会结束 WebSocket 周期。ParseMessages
			// 会保留错误前已解析出的完整消息，因此合法 103 仍能先得到 118 响应。
			continue
		}
	}
}

func IsLikelyClinkHost(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && !strings.Contains(value, "/")
}

func DecodeProxyHandshake(data []byte) (ProxyHandshake, error) {
	var value ProxyHandshake
	if err := json.Unmarshal(data, &value); err != nil {
		return ProxyHandshake{}, err
	}
	return value, nil
}
