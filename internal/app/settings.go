package app

import (
	"errors"
	"fmt"

	"github.com/uvwt/CtyunHelper/internal/storage"
)

type StartupControl interface {
	Enabled() (bool, error)
	SetEnabled(bool) error
}

type GeneralSettings struct {
	AutomationEnabled        bool
	StartOnLogin             bool
	UsagePointsWindowEnabled bool
	UsagePointsWindowStart   string
	UsagePointsWindowEnd     string
}

type SettingsService struct {
	paths        storage.Paths
	startup      StartupControl
	model        *Model
	pointsPolicy *PointsSessionPolicy
}

func NewSettingsService(paths storage.Paths, startup StartupControl, model *Model, pointsPolicy *PointsSessionPolicy) *SettingsService {
	return &SettingsService{paths: paths, startup: startup, model: model, pointsPolicy: pointsPolicy}
}

func (s *SettingsService) Current() (GeneralSettings, error) {
	if s == nil || s.startup == nil || s.model == nil {
		return GeneralSettings{}, fmt.Errorf("app: 通用设置服务未初始化")
	}
	config, err := storage.LoadConfig(s.paths)
	if err != nil {
		return GeneralSettings{}, err
	}
	startOnLogin, err := s.startup.Enabled()
	if err != nil {
		return GeneralSettings{}, err
	}
	return GeneralSettings{
		AutomationEnabled:        config.Automation.Enabled,
		StartOnLogin:             startOnLogin,
		UsagePointsWindowEnabled: config.Automation.UsagePointsWindow.Enabled,
		UsagePointsWindowStart:   config.Automation.UsagePointsWindow.Start,
		UsagePointsWindowEnd:     config.Automation.UsagePointsWindow.End,
	}, nil
}

// Save 同时修改当前用户 Run 注册表和 config.json。注册表先变更；若配置
// 原子写盘失败，则恢复原启动状态。只有两边都成功后才更新进程内 Model。
// 配置部分改走 storage.UpdateConfig：登录提交（SaveAccount）与兑换设置保存
// 并发时，各自的字段修改不再互相覆盖。
func (s *SettingsService) Save(settings GeneralSettings) error {
	if s == nil || s.startup == nil || s.model == nil {
		return fmt.Errorf("app: 通用设置服务未初始化")
	}
	window, err := normalizeUsagePointsWindow(UsagePointsWindow{
		Enabled: settings.UsagePointsWindowEnabled,
		Start:   settings.UsagePointsWindowStart,
		End:     settings.UsagePointsWindowEnd,
	})
	if err != nil {
		return err
	}
	previousStartup, err := s.startup.Enabled()
	if err != nil {
		return err
	}
	startupChanged := previousStartup != settings.StartOnLogin
	if startupChanged {
		if err := s.startup.SetEnabled(settings.StartOnLogin); err != nil {
			return fmt.Errorf("app: 更新登录后自启动: %w", err)
		}
	}

	if err := storage.UpdateConfig(s.paths, func(config *storage.Config) error {
		config.Automation.Enabled = settings.AutomationEnabled
		config.Automation.UsagePointsWindow = storage.UsagePointsWindowConfig{
			Enabled: window.Enabled,
			Start:   window.Start,
			End:     window.End,
		}
		return nil
	}); err != nil {
		if startupChanged {
			if rollbackErr := s.startup.SetEnabled(previousStartup); rollbackErr != nil {
				return errors.Join(err, fmt.Errorf("app: 回滚登录后自启动失败: %w", rollbackErr))
			}
		}
		return err
	}

	s.model.Update(func(state *State) {
		state.AutomationPaused = !settings.AutomationEnabled
	})
	if err := s.pointsPolicy.Update(window); err != nil {
		return fmt.Errorf("app: 更新刷积分时间段: %w", err)
	}
	return nil
}
