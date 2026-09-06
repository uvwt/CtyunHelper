//go:build windows

package winui

import (
	"fmt"

	"github.com/uvwt/CtyunHelper/internal/app"
)

const (
	statusColorDefault uint32 = 0x00202020
	statusColorSuccess uint32 = 0x00006400
	statusColorWarning uint32 = 0x000080E0
	statusColorError   uint32 = 0x000000C0
	statusColorMuted   uint32 = 0x00707070
	statusColorInfo    uint32 = 0x00A06000
)

func homeStatusIndicatorText(text string) string {
	return "● " + text
}

func connectionStatusText(state app.ConnectionState) (string, uint32) {
	switch state {
	case app.ConnectionOnline:
		return "在线", statusColorSuccess
	case app.ConnectionConnecting:
		return "正在连接", statusColorWarning
	case app.ConnectionBackoff:
		return "等待重连", statusColorWarning
	case app.ConnectionPaused:
		return "已暂停", statusColorWarning
	case app.ConnectionAuth:
		return "需要登录", statusColorError
	case app.ConnectionDeviceBind:
		return "需要绑定设备", statusColorWarning
	case app.ConnectionError:
		return "异常", statusColorError
	case app.ConnectionStopped:
		return "等待启动", statusColorMuted
	default:
		return string(state), statusColorDefault
	}
}

func pointsTaskStatusText(status app.PointsTaskStatus) (string, uint32) {
	if !status.Found {
		return "等待刷新", statusColorMuted
	}
	if status.Status == 2 {
		return "已完成", statusColorSuccess
	}
	if status.Progress > 0 {
		return fmt.Sprintf("进行中（进度 %d）", status.Progress), statusColorWarning
	}
	return "待完成", statusColorWarning
}

// usageTaskStatusText 把服务端 currentProgress 的秒数换算成用户可读分钟数。
// 任务是否完成仍只信任官方 status，避免本地进度达到 3600 时提前误判完成。
func usageTaskStatusText(status app.PointsTaskStatus) (string, uint32) {
	if !status.Found {
		return "等待刷新", statusColorMuted
	}
	if status.Status == 2 {
		return "已完成", statusColorSuccess
	}
	if status.Progress > 0 {
		minutes := status.Progress / 60
		if minutes > 60 {
			minutes = 60
		}
		return fmt.Sprintf("进行中 %d/60分", minutes), statusColorWarning
	}
	return "待完成", statusColorWarning
}

func pointsSyncText(status app.JobStatus) string {
	if status.Running {
		return "同步中…"
	}
	if !status.LastRun.IsZero() {
		return status.LastRun.Format("01-02 15:04")
	}
	return "尚未同步"
}

func redeemHomeStatusText(state app.State) (string, uint32) {
	if state.RedeemTask.Running {
		return "运行中", statusColorWarning
	}
	if state.RedeemTask.LastError != "" {
		return "异常：" + state.RedeemTask.LastError, statusColorError
	}
	if !state.RedeemEnabled {
		if state.RedeemSummary != "" {
			if state.RedeemSummary == "未启用" {
				return "未启用", statusColorMuted
			}
			return state.RedeemSummary, statusColorWarning
		}
		return "未启用", statusColorMuted
	}
	if state.AutomationPaused {
		return "已暂停", statusColorWarning
	}
	if !state.RedeemTask.LastRun.IsZero() {
		text := "已完成（" + state.RedeemTask.LastRun.Format("01-02 15:04") + "）"
		if state.RedeemSummary != "" && state.RedeemSummary != "等待兑换计划" {
			text += "；" + state.RedeemSummary
		}
		return text, statusColorSuccess
	}
	return "等待执行", statusColorInfo
}
