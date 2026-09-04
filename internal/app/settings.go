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
	AutomationEnabled bool
	StartOnLogin      bool
}

type SettingsService struct {
	paths   storage.Paths
	startup StartupControl
	model   *Model
}

func NewSettingsService(paths storage.Paths, startup StartupControl, model *Model) *SettingsService {
	return &SettingsService{paths: paths, startup: startup, model: model}
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
		AutomationEnabled: config.Automation.Enabled,
		StartOnLogin:      startOnLogin,
	}, nil
}

// Save 同时修改当前用户 Run 注册表和 config.json。注册表先变更；若配置
// 原子写盘失败，则恢复原启动状态。只有两边都成功后才更新进程内 Model。
func (s *SettingsService) Save(settings GeneralSettings) error {
	if s == nil || s.startup == nil || s.model == nil {
		return fmt.Errorf("app: 通用设置服务未初始化")
	}
	config, err := storage.LoadConfig(s.paths)
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

	config.Automation.Enabled = settings.AutomationEnabled
	if err := storage.SaveConfig(s.paths, config); err != nil {
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
	return nil
}
