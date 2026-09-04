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
		case ConnectionBackoff, ConnectionAuth, ConnectionDeviceBind, ConnectionError:
			r.logger.Warn("connection", "连接状态变化", fields...)
		default:
			r.logger.Info("connection", "连接状态变化", fields...)
		}
	}
	if previous.DesktopName != current.DesktopName && current.DesktopName != "" {
		r.logger.Info("connection", "已选择云电脑", logging.String("desktop", current.DesktopName))
	}
	if previous.Points != current.Points {
		r.logger.Info("points", "积分余额更新", logging.Int("points", current.Points))
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
