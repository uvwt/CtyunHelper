package clink

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/uvwt/CtyunHelper/internal/ctyun/desktop"
	"github.com/uvwt/CtyunHelper/internal/logging"
)

const (
	defaultOrigin                 = "https://pc.ctyun.cn"
	defaultMaxConsecutiveFailures = 3
	defaultClockJumpThreshold     = 30 * time.Second
	defaultMinHealthyDuration     = 15 * time.Second
)

// ErrRouteRefreshRequired 表示当前 Clink 路由或证书可能已失效，或系统经历了休眠唤醒。
// Keepalive 调度层收到该错误后应退出旧 Worker，重新发现云电脑并拉取新连接参数。
var ErrRouteRefreshRequired = errors.New("clink: 需要刷新路由连接")

type cycleOutcome struct {
	protocolVerified bool
	usedProxy        bool
	verifiedDuration time.Duration
}

type SessionMode uint8

const (
	SessionModeKeepalive SessionMode = iota
	SessionModeFormal
)

type FormalAppState uint8

const (
	FormalAppStateUnspecified FormalAppState = iota
	FormalAppStateBack
	FormalAppStateFront
)

type WorkerConfig struct {
	Connection        desktop.ConnectionInfo
	UserID            int64
	UserName          string
	DeviceCode        string
	Mode              SessionMode
	FormalAppState    FormalAppState
	ReconnectInterval time.Duration
	HeartbeatInterval time.Duration
	ErrorBackoff      time.Duration
}

type Worker struct {
	config                 WorkerConfig
	session                *Session
	dialer                 *websocket.Dialer
	transport              *transportPolicy
	now                    func() time.Time
	maxConsecutiveFailures int
	clockJumpThreshold     time.Duration
	healthyCycleObserved   atomic.Bool
}

func NewWorker(config WorkerConfig, notify func(Snapshot)) *Worker {
	if config.ReconnectInterval <= 0 {
		config.ReconnectInterval = 60 * time.Second
	}
	if config.HeartbeatInterval <= 0 {
		config.HeartbeatInterval = 5 * time.Second
	}
	if config.ErrorBackoff <= 0 {
		config.ErrorBackoff = 5 * time.Second
	}
	worker := &Worker{
		config:  config,
		session: NewSession(notify),
		dialer: &websocket.Dialer{
			HandshakeTimeout: 15 * time.Second,
			Subprotocols:     []string{"binary"},
			Proxy:            clinkProxy,
			TLSClientConfig:  newClinkTLSConfig(config.Connection.ClinkLVSOutHost),
		},
		now:                    time.Now,
		maxConsecutiveFailures: defaultMaxConsecutiveFailures,
		clockJumpThreshold:     defaultClockJumpThreshold,
	}
	worker.transport = newTransportPolicy(worker.currentTime)
	return worker
}

func (w *Worker) Snapshot() Snapshot {
	return w.session.Snapshot()
}

// HadHealthyCycle 返回当前 Worker 是否至少完成过一次有效保活周期。
// Keepalive 层据此判断重新拉取路由后是否应重置递增退避。
func (w *Worker) HadHealthyCycle() bool {
	return w.healthyCycleObserved.Load()
}

func (w *Worker) currentTime() time.Time {
	if w.now != nil {
		return w.now()
	}
	return time.Now()
}

func (w *Worker) minHealthyDuration() time.Duration {
	half := w.config.ReconnectInterval / 2
	if half > 0 && half < defaultMinHealthyDuration {
		return half
	}
	return defaultMinHealthyDuration
}

func (w *Worker) maxFailuresBeforeRefresh() int {
	if w.maxConsecutiveFailures > 0 {
		return w.maxConsecutiveFailures
	}
	return defaultMaxConsecutiveFailures
}

func (w *Worker) dialWebSocket(ctx context.Context, endpoint string, headers http.Header) (*websocket.Conn, *http.Response, string, error) {
	policy := w.transport
	if policy == nil {
		policy = newTransportPolicy(w.currentTime)
		w.transport = policy
	}
	ws, resp, transport, err := policy.dial(ctx, w.dialer, endpoint, headers)
	if err == nil {
		w.session.setTransport(transport)
	}
	return ws, resp, transport, err
}

// Run 是 Clink 保活的长期主流程。每个连接周期复现官方代理握手所需消息，
// 服务端校验与用户信息响应都在同一个读循环中完成；UI 和调度层不接触 WebSocket。
// 若连续多轮快速失败、未收到有效保活校验或检测到休眠唤醒/时钟跳变，
// 则主动返回 ErrRouteRefreshRequired 交给 Keepalive 重新刷新路由与凭据。
func (w *Worker) Run(ctx context.Context) error {
	consecutiveFailures := 0
	for {
		if ctx.Err() != nil {
			_ = w.session.Transition(StateStopped, nil)
			return ctx.Err()
		}

		if err := w.session.Transition(StateResolving, nil); err != nil {
			return err
		}
		cycleStart := w.currentTime()
		outcome, err := w.runCycle(ctx)
		cycleEnd := w.currentTime()
		if ctx.Err() != nil {
			_ = w.session.Transition(StateStopped, nil)
			return ctx.Err()
		}
		if err != nil && outcome.usedProxy && isProxyTransportTimeout(err) && w.transport != nil {
			// 经系统代理已建立隧道但随后读/写超时（例如本地代理卡死），
			// 下一轮直接切入直连冷却期，不再反复被代理拖断。
			w.transport.markProxyFailed()
		}

		clockJumped := detectClockJump(cycleStart, cycleEnd, w.config.ReconnectInterval+30*time.Second, w.clockJumpThreshold)
		healthy := isHealthyCycle(outcome, err, w.minHealthyDuration())
		if healthy {
			w.healthyCycleObserved.Store(true)
			consecutiveFailures = 0
		} else {
			consecutiveFailures++
		}

		if refreshErr := w.routeRefreshError(clockJumped, healthy, consecutiveFailures, err); refreshErr != nil {
			_ = w.session.Transition(StateBackoff, refreshErr)
			return refreshErr
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

		backoffStart := w.currentTime()
		timer := time.NewTimer(w.config.ErrorBackoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = w.session.Transition(StateStopped, nil)
			return ctx.Err()
		case <-timer.C:
		}
		backoffEnd := w.currentTime()
		if detectClockJump(backoffStart, backoffEnd, w.config.ErrorBackoff, w.clockJumpThreshold) {
			refreshErr := fmt.Errorf("%w: 退避期间检测到系统时钟跳变或休眠唤醒", ErrRouteRefreshRequired)
			_ = w.session.Transition(StateBackoff, refreshErr)
			return refreshErr
		}
	}
}

func (w *Worker) routeRefreshError(clockJumped, healthy bool, consecutiveFailures int, cycleErr error) error {
	if clockJumped {
		if cycleErr != nil {
			return fmt.Errorf("%w: 检测到系统时钟跳变或休眠唤醒: %w", ErrRouteRefreshRequired, cycleErr)
		}
		return fmt.Errorf("%w: 检测到系统时钟跳变或休眠唤醒", ErrRouteRefreshRequired)
	}
	if healthy || consecutiveFailures < w.maxFailuresBeforeRefresh() {
		return nil
	}
	if cycleErr != nil {
		return fmt.Errorf("%w: 连续 %d 次连接周期异常: %w", ErrRouteRefreshRequired, consecutiveFailures, cycleErr)
	}
	return fmt.Errorf("%w: 连续 %d 次连接周期未收到有效保活校验", ErrRouteRefreshRequired, consecutiveFailures)
}

func isHealthyCycle(outcome cycleOutcome, cycleErr error, minHealthyDuration time.Duration) bool {
	if !outcome.protocolVerified {
		return false
	}
	if cycleErr == nil {
		return true
	}
	return outcome.verifiedDuration >= minHealthyDuration
}

// detectClockJump 同时检查 wall/monotonic 偏差与单次操作真实 wall 耗时，
// 覆盖 Windows 休眠唤醒后单调时钟暂停或同步推进两种场景。
func detectClockJump(start, end time.Time, maxExpected, threshold time.Duration) bool {
	if threshold <= 0 {
		return false
	}
	wallElapsed := end.Round(0).Sub(start.Round(0))
	monoElapsed := end.Sub(start)
	skew := wallElapsed - monoElapsed
	if skew < 0 {
		skew = -skew
	}
	if skew >= threshold {
		return true
	}
	if maxExpected > 0 && wallElapsed > maxExpected+threshold {
		return true
	}
	return false
}

func (w *Worker) runCycle(ctx context.Context) (cycleOutcome, error) {
	connection := w.config.Connection
	if connection.DesktopID == 0 || connection.ClinkLVSOutHost == "" {
		return cycleOutcome{}, fmt.Errorf("clink: 连接参数不完整")
	}
	if w.config.Mode == SessionModeFormal {
		return w.runFormalCycle(ctx)
	}
	endpoint := url.URL{
		Scheme: "wss",
		Host:   connection.ClinkLVSOutHost,
		Path:   fmt.Sprintf("/clinkProxy/%d/MAIN", connection.DesktopID),
	}
	return w.runCycleWithURL(ctx, endpoint.String())
}

func (w *Worker) runCycleWithURL(ctx context.Context, endpoint string) (cycleOutcome, error) {
	var outcome cycleOutcome
	var verifiedAt time.Time
	markVerified := func() {
		if !outcome.protocolVerified {
			outcome.protocolVerified = true
			verifiedAt = w.currentTime()
		}
	}
	finish := func(err error) (cycleOutcome, error) {
		if outcome.protocolVerified {
			outcome.verifiedDuration = w.currentTime().Sub(verifiedAt)
		}
		return outcome, err
	}

	connection := w.config.Connection
	if err := w.session.Transition(StateConnecting, nil); err != nil {
		return finish(err)
	}
	headers := http.Header{"Origin": {defaultOrigin}}
	ws, response, transport, err := w.dialWebSocket(ctx, endpoint, headers)
	outcome.usedProxy = transport == TransportProxy
	if err != nil {
		if response != nil {
			return finish(fmt.Errorf("clink: WebSocket 连接失败 HTTP %d: %w", response.StatusCode, err))
		}
		return finish(fmt.Errorf("clink: WebSocket 连接失败: %w", err))
	}
	defer ws.Close()

	if err := w.session.Transition(StateHandshaking, nil); err != nil {
		return finish(err)
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
		return finish(fmt.Errorf("clink: 编码代理握手: %w", err))
	}
	if err := ws.WriteMessage(websocket.TextMessage, payload); err != nil {
		return finish(fmt.Errorf("clink: 发送代理握手: %w", err))
	}

	select {
	case <-ctx.Done():
		return finish(ctx.Err())
	case <-time.After(500 * time.Millisecond):
	}
	if err := ws.WriteMessage(websocket.BinaryMessage, InitialPayload()); err != nil {
		return finish(fmt.Errorf("clink: 发送初始 REDQ 帧: %w", err))
	}
	if err := w.session.Transition(StateOnline, nil); err != nil {
		return finish(err)
	}

	cycleCtx, cancel := context.WithTimeout(ctx, w.config.ReconnectInterval)
	defer cancel()
	closed := make(chan struct{})
	go func() {
		defer logging.RecoverPanic("clink.cycle_closer")
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
				return finish(nil)
			}
			return finish(fmt.Errorf("clink: 读取 WebSocket: %w", err))
		}
		if messageType != websocket.BinaryMessage || len(data) == 0 {
			continue
		}
		if IsREDQ(data) {
			w.session.recordProtocolEvent(func(snapshot *Snapshot) {
				snapshot.REDQChallenges++
			})
			response, err := BuildREDQResponse(data)
			if err != nil {
				return finish(err)
			}
			if err := ws.WriteMessage(websocket.BinaryMessage, response); err != nil {
				return finish(fmt.Errorf("clink: 发送 REDQ 响应: %w", err))
			}
			w.session.recordProtocolEvent(func(snapshot *Snapshot) {
				snapshot.REDQResponses++
			})
			markVerified()
			continue
		}

		messages, parseErr := ParseMessages(data)
		for _, message := range messages {
			if message.Type != 103 {
				continue
			}
			w.session.recordProtocolEvent(func(snapshot *Snapshot) {
				snapshot.UserInfoRequests++
			})
			userInfo, err := BuildUserInfoMessage(w.config.UserID, w.config.UserName)
			if err != nil {
				return finish(fmt.Errorf("clink: 编码用户信息: %w", err))
			}
			if err := ws.WriteMessage(websocket.BinaryMessage, userInfo); err != nil {
				return finish(fmt.Errorf("clink: 发送用户信息: %w", err))
			}
			w.session.recordProtocolEvent(func(snapshot *Snapshot) {
				snapshot.UserInfoResponses++
			})
			markVerified()
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
