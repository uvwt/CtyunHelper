//go:build windows

package winui

import (
	"testing"
	"time"

	"github.com/uvwt/CtyunHelper/internal/app"
)

func TestJobStatusTextShowsCompletedAfterSuccessfulRun(t *testing.T) {
	lastRun := time.Date(2026, 9, 5, 16, 30, 0, 0, time.Local)
	text, color := jobStatusText(app.JobStatus{LastRun: lastRun}, "运行中", false)
	if text != "已完成（09-05 16:30）" {
		t.Fatalf("text = %q", text)
	}
	if color != statusColorSuccess {
		t.Fatalf("color = %#x", color)
	}
}

func TestJobStatusTextKeepsErrorAheadOfLastRun(t *testing.T) {
	text, color := jobStatusText(app.JobStatus{
		LastRun:   time.Date(2026, 9, 5, 16, 30, 0, 0, time.Local),
		LastError: "请求失败",
	}, "运行中", false)
	if text != "异常：请求失败" {
		t.Fatalf("text = %q", text)
	}
	if color != statusColorError {
		t.Fatalf("color = %#x", color)
	}
}

func TestUsageStatusTextShowsCompleted(t *testing.T) {
	text, color := usageStatusText(app.UsageTaskStatus{Found: true, Status: 2, Progress: 60})
	if text != "已完成" || color != statusColorSuccess {
		t.Fatalf("status = %q color=%#x", text, color)
	}
}

func TestHomeStatusIndicatorTextUsesSolidDot(t *testing.T) {
	if got := homeStatusIndicatorText("已完成"); got != "● 已完成" {
		t.Fatalf("indicator = %q", got)
	}
}

func TestAIHomeStatusUsesServerTaskCompletionAfterRestart(t *testing.T) {
	state := app.State{
		AITaskCompleted: true,
		AITask: app.JobStatus{
			NextRun: time.Date(2026, 9, 5, 20, 0, 0, 0, time.Local),
		},
	}
	text, color := aiHomeStatusText(state)
	if text != "已完成；下次 09-05 20:00" || color != statusColorSuccess {
		t.Fatalf("status = %q color=%#x", text, color)
	}
}
