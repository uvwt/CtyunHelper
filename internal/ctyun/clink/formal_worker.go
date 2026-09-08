package clink

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/uvwt/CtyunHelper/internal/logging"
)

// runFormalCycle 在已经验证过的轻量 Clink MAIN 链路上继续完成正式登录。
// 实机验证表明：112 登录成功并维持 MAIN heartbeat 后，服务端会延迟批量累计
// “使用1小时”进度；因此这里不伪造 DISPLAY/INPUTS 子通道。
func (w *Worker) runFormalCycle(ctx context.Context) error {
	connection := w.config.Connection
	if connection.Token == "" || connection.TenantMemberAccount == "" || w.config.DeviceCode == "" {
		return fmt.Errorf("clink: 正式会话缺少 token、tenantMemberAccount 或 deviceCode")
	}
	endpoint := (&url.URL{
		Scheme: "wss",
		Host:   connection.ClinkLVSOutHost,
		Path:   fmt.Sprintf("/clinkProxy/%d/MAIN", connection.DesktopID),
	}).String()
	return w.runFormalCycleWithURL(ctx, endpoint)
}

func (w *Worker) runFormalCycleWithURL(ctx context.Context, endpoint string) error {
	connection := w.config.Connection
	if err := w.session.Transition(StateConnecting, nil); err != nil {
		return err
	}
	headers := http.Header{"Origin": {defaultOrigin}}
	ws, response, err := w.dialer.DialContext(ctx, endpoint, headers)
	if err != nil {
		if response != nil {
			return fmt.Errorf("clink: 正式 MAIN WebSocket 连接失败 HTTP %d: %w", response.StatusCode, err)
		}
		return fmt.Errorf("clink: 正式 MAIN WebSocket 连接失败: %w", err)
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
		return fmt.Errorf("clink: 编码正式 MAIN 代理握手: %w", err)
	}

	var writeMu sync.Mutex
	write := func(messageType int, data []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return ws.WriteMessage(messageType, data)
	}
	if err := write(websocket.TextMessage, payload); err != nil {
		return fmt.Errorf("clink: 发送正式 MAIN 代理握手: %w", err)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(500 * time.Millisecond):
	}
	if err := write(websocket.BinaryMessage, InitialPayload()); err != nil {
		return fmt.Errorf("clink: 发送正式 MAIN 初始 REDQ: %w", err)
	}

	cycleCtx, cancel := context.WithTimeout(ctx, w.config.ReconnectInterval)
	defer cancel()
	closed := make(chan struct{})
	go func() {
		defer logging.RecoverPanic("clink.formal_cycle_closer")
		select {
		case <-cycleCtx.Done():
			_ = ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "cycle reset"), time.Now().Add(time.Second))
			_ = ws.Close()
		case <-closed:
		}
	}()
	defer close(closed)

	loginSent := false
	loginAccepted := false
	heartbeatStarted := false
	heartbeatErr := make(chan error, 1)
	heartbeatCtx, stopHeartbeat := context.WithCancel(cycleCtx)
	defer stopHeartbeat()

	for {
		messageType, data, err := ws.ReadMessage()
		if err != nil {
			if cycleCtx.Err() != nil {
				return nil
			}
			select {
			case heartbeatFailure := <-heartbeatErr:
				return heartbeatFailure
			default:
			}
			return fmt.Errorf("clink: 读取正式 MAIN WebSocket: %w", err)
		}
		if messageType != websocket.BinaryMessage || len(data) == 0 {
			continue
		}
		if IsREDQ(data) {
			w.session.recordProtocolEvent(func(snapshot *Snapshot) {
				snapshot.REDQChallenges++
			})
			responsePayload, err := BuildREDQResponse(data)
			if err != nil {
				return err
			}
			if err := write(websocket.BinaryMessage, responsePayload); err != nil {
				return fmt.Errorf("clink: 发送正式 MAIN REDQ 响应: %w", err)
			}
			w.session.recordProtocolEvent(func(snapshot *Snapshot) {
				snapshot.REDQResponses++
			})
			continue
		}

		messages, parseErr := ParseMessages(data)
		for _, message := range messages {
			switch message.Type {
			case 103:
				w.session.recordProtocolEvent(func(snapshot *Snapshot) {
					snapshot.UserInfoRequests++
				})
				userInfo, err := BuildUserInfoMessage(w.config.UserID, w.config.UserName)
				if err != nil {
					return fmt.Errorf("clink: 编码正式 MAIN 用户信息: %w", err)
				}
				if err := write(websocket.BinaryMessage, userInfo); err != nil {
					return fmt.Errorf("clink: 发送正式 MAIN 118 用户信息: %w", err)
				}
				w.session.recordProtocolEvent(func(snapshot *Snapshot) {
					snapshot.UserInfoResponses++
				})

				if !loginSent {
					login, err := BuildClientLoginMessage(
						connection.DesktopID,
						connection.Token,
						w.config.DeviceCode,
						connection.TenantMemberAccount,
					)
					if err != nil {
						return err
					}
					if err := write(websocket.BinaryMessage, login); err != nil {
						return fmt.Errorf("clink: 发送 112 正式 MAIN 登录: %w", err)
					}
					loginSent = true
					_ = ws.SetReadDeadline(time.Now().Add(15 * time.Second))
					w.session.recordProtocolEvent(func(snapshot *Snapshot) {
						snapshot.ClientLogins++
					})
				}

			case msgMainLoginResponse:
				if !loginSent {
					return fmt.Errorf("clink: 在 112 正式 MAIN 登录前收到 136 响应")
				}
				if loginAccepted {
					continue
				}
				result, err := ParseLoginResult(message.Data)
				if err != nil {
					return err
				}
				w.session.recordProtocolEvent(func(snapshot *Snapshot) {
					snapshot.LoginResponses++
					snapshot.LastLoginResult = result
				})
				if result != 0 {
					return fmt.Errorf("clink: 136 正式 MAIN 登录失败 result=%d", result)
				}
				loginAccepted = true
				_ = ws.SetReadDeadline(time.Time{})
				switch w.config.FormalAppState {
				case FormalAppStateBack:
					// 官方客户端把 MAIN 的应用后台态编码为独立的 113 空消息。
					// 登录一旦被服务端接受就立即声明 back，尽量避免 Helper 成为前台会话 owner。
					if err := write(websocket.BinaryMessage, BuildAppBackMessage()); err != nil {
						return fmt.Errorf("clink: 发送 113 app status back: %w", err)
					}
					w.session.recordProtocolEvent(func(snapshot *Snapshot) {
						snapshot.AppBackRequests++
					})
				case FormalAppStateFront:
					if err := write(websocket.BinaryMessage, BuildAppFrontMessage()); err != nil {
						return fmt.Errorf("clink: 发送 114 app status front: %w", err)
					}
					w.session.recordProtocolEvent(func(snapshot *Snapshot) {
						snapshot.AppFrontRequests++
					})
				}
				if err := write(websocket.BinaryMessage, BuildAttachChannelsMessage()); err != nil {
					return fmt.Errorf("clink: 发送 104 attach channels: %w", err)
				}
				if err := write(websocket.BinaryMessage, BuildClientVersionMessage()); err != nil {
					return fmt.Errorf("clink: 发送 116 client version: %w", err)
				}
				w.session.recordProtocolEvent(func(snapshot *Snapshot) {
					snapshot.AttachRequests++
				})
				if err := w.session.Transition(StateOnline, nil); err != nil {
					return err
				}
				if !heartbeatStarted {
					heartbeatStarted = true
					go w.runFormalHeartbeat(heartbeatCtx, ws, &writeMu, heartbeatErr)
				}
			}
		}
		if parseErr != nil {
			// 单个未知或残缺的普通 Clink 帧不应结束已经建立的正式 MAIN 会话。
			continue
		}
	}
}

func (w *Worker) runFormalHeartbeat(ctx context.Context, ws *websocket.Conn, writeMu *sync.Mutex, result chan<- error) {
	defer logging.RecoverPanic("clink.formal_heartbeat")
	ticker := time.NewTicker(w.config.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			writeMu.Lock()
			err := ws.WriteMessage(websocket.BinaryMessage, BuildHeartbeatMessage())
			writeMu.Unlock()
			if err != nil {
				select {
				case result <- fmt.Errorf("clink: 发送正式 MAIN heartbeat: %w", err):
				default:
				}
				_ = ws.Close()
				return
			}
			w.session.recordProtocolEvent(func(snapshot *Snapshot) {
				snapshot.Heartbeats++
			})
		}
	}
}
