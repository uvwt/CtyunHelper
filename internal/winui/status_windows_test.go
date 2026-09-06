//go:build windows

package winui

import (
	"testing"
	"time"

	"github.com/uvwt/CtyunHelper/internal/app"
)

func TestPointsSyncTextShowsOnlyLastSyncTime(t *testing.T) {
	lastRun := time.Date(2026, 9, 5, 16, 30, 0, 0, time.Local)
	text := pointsSyncText(app.JobStatus{LastRun: lastRun})
	if text != "09-05 16:30" {
		t.Fatalf("text = %q", text)
	}
}

func TestPointsSyncTextShowsRunningAndInitialState(t *testing.T) {
	if got := pointsSyncText(app.JobStatus{Running: true}); got != "同步中…" {
		t.Fatalf("running = %q", got)
	}
	if got := pointsSyncText(app.JobStatus{}); got != "尚未同步" {
		t.Fatalf("initial = %q", got)
	}
}

func TestPointsTaskStatusTextShowsCompleted(t *testing.T) {
	text, color := pointsTaskStatusText(app.PointsTaskStatus{Found: true, Status: 2, Progress: 3600})
	if text != "已完成" || color != statusColorSuccess {
		t.Fatalf("status = %q color=%#x", text, color)
	}
}

func TestPointsTaskStatusTextShowsProgressAndPending(t *testing.T) {
	text, color := pointsTaskStatusText(app.PointsTaskStatus{Found: true, Status: 0, Progress: 35})
	if text != "进行中（进度 35）" || color != statusColorWarning {
		t.Fatalf("progress status = %q color=%#x", text, color)
	}
	text, color = pointsTaskStatusText(app.PointsTaskStatus{Found: true, Status: 0})
	if text != "待完成" || color != statusColorWarning {
		t.Fatalf("pending status = %q color=%#x", text, color)
	}
}

func TestUsageTaskStatusTextShowsMinutesWithoutLocallyCompleting(t *testing.T) {
	text, color := usageTaskStatusText(app.PointsTaskStatus{Found: true, Status: 0, Progress: 1100})
	if text != "进行中 18/60分" || color != statusColorWarning {
		t.Fatalf("usage status = %q color=%#x", text, color)
	}
	text, color = usageTaskStatusText(app.PointsTaskStatus{Found: true, Status: 0, Progress: 3600})
	if text != "进行中 60/60分" || color != statusColorWarning {
		t.Fatalf("unfinished 3600 status = %q color=%#x", text, color)
	}
	text, color = usageTaskStatusText(app.PointsTaskStatus{Found: true, Status: 2, Progress: 3600})
	if text != "已完成" || color != statusColorSuccess {
		t.Fatalf("completed usage status = %q color=%#x", text, color)
	}
}

func TestHomeStatusIndicatorTextUsesSolidDot(t *testing.T) {
	if got := homeStatusIndicatorText("已完成"); got != "● 已完成" {
		t.Fatalf("indicator = %q", got)
	}
}

func TestRedeemNextRunText(t *testing.T) {
	nextRun := time.Date(2026, 9, 7, 4, 5, 0, 0, time.Local)
	if got := redeemNextRunText(app.State{RedeemEnabled: true, RedeemTask: app.JobStatus{NextRun: nextRun}}); got != "09-07 04:05" {
		t.Fatalf("next run = %q", got)
	}
	if got := redeemNextRunText(app.State{}); got != "未启用" {
		t.Fatalf("disabled = %q", got)
	}
	if got := redeemNextRunText(app.State{RedeemEnabled: true}); got != "计算中" {
		t.Fatalf("pending schedule = %q", got)
	}
	if got := redeemNextRunText(app.State{RedeemSummary: "上次兑换结果不确定，已停止自动兑换"}); got != "待确认上一笔" {
		t.Fatalf("pending redeem = %q", got)
	}
}
