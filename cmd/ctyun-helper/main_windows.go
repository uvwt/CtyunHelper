//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/uvwt/CtyunHelper/internal/app"
	"github.com/uvwt/CtyunHelper/internal/automation"
	"github.com/uvwt/CtyunHelper/internal/ctyun/auth"
	"github.com/uvwt/CtyunHelper/internal/ctyun/desktop"
	"github.com/uvwt/CtyunHelper/internal/ctyun/eai"
	"github.com/uvwt/CtyunHelper/internal/ctyun/points"
	"github.com/uvwt/CtyunHelper/internal/logging"
	"github.com/uvwt/CtyunHelper/internal/storage"
	"github.com/uvwt/CtyunHelper/internal/winui"
)

func main() {
	if err := run(); err != nil {
		winui.ShowError("CtyunHelper", err.Error())
	}
}

func run() error {
	return winui.Run(buildRuntime)
}

func buildRuntime() (*app.Runtime, error) {
	paths, err := storage.ResolvePaths()
	if err != nil {
		return nil, fmt.Errorf("初始化用户目录: %w", err)
	}
	config, err := storage.LoadConfig(paths)
	if err != nil {
		return nil, err
	}
	changed, err := storage.EnsureWindowsDevice(&config, nil, nil)
	if err != nil {
		return nil, err
	}
	if changed {
		if err := storage.SaveConfig(paths, config); err != nil {
			return nil, err
		}
	}

	device := auth.DeviceIdentity{
		Code: config.Device.Code, Name: config.Device.Name, Model: config.Device.Model,
		Type: config.Device.Type, CreatedAt: config.Device.CreatedAt,
	}
	authClient := auth.NewClient(device, auth.ClientOptions{})
	model := app.NewModel(app.State{
		Account:          config.Account,
		Connection:       app.ConnectionAuth,
		AutomationPaused: !config.Automation.Enabled,
	})
	accountStore := storage.NewAccountStore(paths)
	desktopClient := desktop.NewClient(authClient, desktop.ClientOptions{})
	keepalive := app.NewKeepalive(authClient, desktopClient, model)
	pointsClient := points.NewClient(authClient, points.ClientOptions{})
	eaiClient, err := eai.NewClient(authClient, eai.ClientOptions{})
	if err != nil {
		return nil, err
	}
	policy := automation.Policy{
		MaxAI:           config.Safety.MaxAI,
		MaxLogin:        config.Safety.MaxLogin,
		MaxRedeem:       config.Safety.MaxRedeem,
		MaxFailures:     config.Safety.MaxFailures,
		FailureCooldown: time.Duration(config.Safety.CooldownHours) * time.Hour,
	}
	var safetyState automation.SafetyState
	if _, err := storage.LoadStateJSON(paths, "safety.json", &safetyState); err != nil {
		return nil, err
	}
	var redeemState automation.RedeemState
	if _, err := storage.LoadStateJSON(paths, "redeem.json", &redeemState); err != nil {
		return nil, err
	}
	guard := automation.NewGuard(policy, safetyState, automation.GuardOptions{
		Save: func(state automation.SafetyState) error {
			return storage.SaveStateJSON(paths, "safety.json", state)
		},
	})
	authFlow := app.NewAuthFlow(authClient, accountStore, model, guard)
	aiJob := automation.NewAIJob(pointsClient, eaiClient, guard, "你好")
	pointsJob := automation.NewPointsJob(pointsClient, automation.PointsJobOptions{})
	redeemPlan := automation.RedeemPlan{
		Enabled: config.Redeem.Enabled, Account: config.Redeem.Account, DesktopID: config.Redeem.DesktopID,
		ProductID: config.Redeem.ProductID, ProductName: config.Redeem.ProductName,
		ProductType: config.Redeem.ProductType, CostPoints: config.Redeem.CostPoints,
		MaxRedeemTimes: config.Redeem.MaxRedeemTimes, ScheduleType: config.Redeem.ScheduleType,
		IntervalDays: config.Redeem.IntervalDays, MonthlyDays: config.Redeem.MonthlyDays,
	}
	redeemJob := automation.NewRedeemJob(pointsClient, guard, redeemPlan, redeemState, automation.RedeemJobOptions{
		SaveState: func(state automation.RedeemState) error {
			return storage.SaveStateJSON(paths, "redeem.json", state)
		},
	})
	taskAutomation, err := app.NewTaskAutomationWithOptions(model, app.TaskAutomationOptions{
		AIJob: aiJob, PointsJob: pointsJob, RedeemJob: redeemJob,
	})
	if err != nil {
		return nil, err
	}
	redeemSettings := app.NewRedeemSettingsService(paths, pointsClient, taskAutomation, model)
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("获取程序路径: %w", err)
	}
	startup, err := storage.NewStartup(executable)
	if err != nil {
		return nil, err
	}
	settings := app.NewSettingsService(paths, startup, model)
	logger, err := logging.New(logging.Options{Path: filepath.Join(paths.LogDir, "CtyunHelper.log")})
	if err != nil {
		return nil, err
	}
	runtime := app.NewRuntime(model, authFlow, keepalive, taskAutomation, app.RuntimeOptions{
		RedeemSettings: redeemSettings,
		Settings:       settings,
		Logger:         logger,
	})

	// Profile 恢复失败不阻止 UI 启动；AuthFlow 已把可操作错误写入 Model，用户可以重新登录。
	_, _ = runtime.Restore(config.Account)
	return runtime, nil
}
