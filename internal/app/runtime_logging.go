package app

import (
	"context"

	"github.com/uvwt/CtyunHelper/internal/logging"
)

func (r *Runtime) observeState(ctx context.Context, previous State, events <-chan Event, unsubscribe func()) {
	defer unsubscribe()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if event.Type != EventStateChanged {
				continue
			}
			current, ok := event.Data.(State)
			if !ok {
				continue
			}
			r.logStateTransition(previous, current)
			previous = current
		}
	}
}

func (r *Runtime) logStateTransition(previous, current State) {
	if r.logger == nil {
		return
	}
	if previous.Connection != current.Connection {
		fields := []logging.Field{logging.String("state", string(current.Connection))}
		switch current.Connection {
		case ConnectionAuth, ConnectionDeviceBind, ConnectionError:
			r.logger.Warn("connection", "连接状态变化", fields...)
		default:
			// Clink 每个健康周期都会主动进入 backoff 等待下一次约 60 秒重连，
			// 这是正常生命周期而不是告警；真正的连接错误会通过 LastError 单独记录。
			r.logger.Info("connection", "连接状态变化", fields...)
		}
	}
	if previous.DesktopName != current.DesktopName && current.DesktopName != "" {
		r.logger.Info("connection", "已选择云电脑", logging.String("desktop", current.DesktopName))
	}
	if previous.REDQChallenges != current.REDQChallenges {
		r.logger.Info("clink", "收到 REDQ 保活校验", logging.Int("count", current.REDQChallenges))
	}
	if previous.REDQResponses != current.REDQResponses {
		r.logger.Info("clink", "发送 REDQ 保活响应成功", logging.Int("count", current.REDQResponses))
	}
	if previous.UserInfoRequests != current.UserInfoRequests {
		r.logger.Info("clink", "收到 103 用户信息请求", logging.Int("count", current.UserInfoRequests))
	}
	if previous.UserInfoResponses != current.UserInfoResponses {
		r.logger.Info("clink", "发送 118 用户信息响应成功", logging.Int("count", current.UserInfoResponses))
	}
	if previous.ClientLogins != current.ClientLogins {
		r.logger.Info("clink", "发送 112 正式 MAIN 登录", logging.Int("count", current.ClientLogins))
	}
	if previous.LoginResponses != current.LoginResponses {
		r.logger.Info("clink", "收到 136 正式 MAIN 登录响应",
			logging.Int("count", current.LoginResponses),
			logging.Int("result", int(current.LastLoginResult)),
		)
	}
	if previous.AppBackRequests != current.AppBackRequests {
		r.logger.Info("clink", "发送 113 app status back", logging.Int("count", current.AppBackRequests))
	}
	if previous.AttachRequests != current.AttachRequests {
		r.logger.Info("clink", "发送 104 attach channels", logging.Int("count", current.AttachRequests))
	}
	if previous.Heartbeats != current.Heartbeats && (current.Heartbeats == 1 || current.Heartbeats%12 == 0) {
		r.logger.Info("clink", "发送正式会话 heartbeat", logging.Int("count", current.Heartbeats))
	}
	if previous.Points != current.Points {
		r.logger.Info("points", "积分余额更新", logging.Int("points", current.Points))
	}
	if previous.UsageTask != current.UsageTask && current.UsageTask.Found {
		r.logger.Info("points", "使用1小时进度更新",
			logging.Int("status", current.UsageTask.Status),
			logging.Int("progress", current.UsageTask.Progress),
		)
	}
	if previous.AutomationPaused != current.AutomationPaused {
		if current.AutomationPaused {
			r.logger.Info("automation", "自动任务已暂停")
		} else {
			r.logger.Info("automation", "自动任务已启用")
		}
	}
	logJobTransition(r.logger, "ai", previous.AITask, current.AITask)
	logJobTransition(r.logger, "points", previous.PointsTask, current.PointsTask)
	logJobTransition(r.logger, "redeem", previous.RedeemTask, current.RedeemTask)
	if previous.RedeemSummary != current.RedeemSummary && current.RedeemSummary != "" {
		r.logger.Info("redeem", current.RedeemSummary)
	}
	if previous.LastError != current.LastError && current.LastError != "" {
		r.logger.Warn("app", current.LastError)
	}
}

func logJobTransition(logger *logging.Logger, component string, previous, current JobStatus) {
	if logger == nil {
		return
	}
	if !previous.Running && current.Running {
		logger.Info(component, "任务开始")
	}
	if previous.Running && !current.Running {
		if current.LastError != "" {
			logger.Error(component, "任务失败", logging.String("error", current.LastError))
		} else {
			logger.Info(component, "任务完成")
		}
	} else if previous.LastError != current.LastError && current.LastError != "" {
		logger.Error(component, "任务异常", logging.String("error", current.LastError))
	}
}
