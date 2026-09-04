package storage

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const CredentialTarget = "CtyunHelper/account"

type Config struct {
	Account    string           `json:"account"`
	Device     DeviceConfig     `json:"device"`
	Automation AutomationConfig `json:"automation"`
	Safety     SafetyConfig     `json:"safety"`
}

type DeviceConfig struct {
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Model     string    `json:"model"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"createdAt"`
}

type AutomationConfig struct {
	Enabled bool `json:"enabled"`
}

type SafetyConfig struct {
	MaxAI         int `json:"maxAI"`
	MaxLogin      int `json:"maxLogin"`
	MaxRedeem     int `json:"maxRedeem"`
	MaxFailures   int `json:"maxFailures"`
	CooldownHours int `json:"cooldownHours"`
}

func DefaultConfig() Config {
	return Config{
		Automation: AutomationConfig{Enabled: true},
		Safety: SafetyConfig{
			MaxAI: 2, MaxLogin: 2, MaxRedeem: 1, MaxFailures: 3, CooldownHours: 6,
		},
	}
}

func LoadConfig(paths Paths) (Config, error) {
	path := filepath.Join(paths.ConfigDir, "config.json")
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return DefaultConfig(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("storage: 读取配置: %w", err)
	}
	config := DefaultConfig()
	if err := json.Unmarshal(raw, &config); err != nil {
		return Config{}, fmt.Errorf("storage: 解析配置: %w", err)
	}
	return config, nil
}

func SaveConfig(paths Paths, config Config) error {
	raw, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("storage: 编码配置: %w", err)
	}
	return writeAtomic(filepath.Join(paths.ConfigDir, "config.json"), raw, 0o600)
}

// EnsureWindowsDevice 只在首次没有 DeviceCode 时生成官方 Windows 新安装形态 ctyun_<32 chars>。
// 已存在 Code 时永远不替换；其余描述字段缺失只补默认值，不改变设备身份。
func EnsureWindowsDevice(config *Config, source io.Reader, now func() time.Time) (bool, error) {
	if config == nil {
		return false, fmt.Errorf("storage: config 不能为空")
	}
	if source == nil {
		source = rand.Reader
	}
	if now == nil {
		now = time.Now
	}
	changed := false
	if config.Device.Code == "" {
		randomValue, err := randomAlphaNumeric(source, 32)
		if err != nil {
			return false, err
		}
		config.Device.Code = "ctyun_" + randomValue
		config.Device.CreatedAt = now()
		changed = true
	}
	if config.Device.Name == "" {
		config.Device.Name = "Windows"
		changed = true
	}
	if config.Device.Model == "" {
		config.Device.Model = "windows"
		changed = true
	}
	if config.Device.Type == "" {
		config.Device.Type = "25"
		changed = true
	}
	if config.Device.Type != "25" {
		return false, fmt.Errorf("storage: 已保存的设备类型 %q 不是 Windows PC(25)，拒绝静默修改", config.Device.Type)
	}
	return changed, nil
}

func randomAlphaNumeric(source io.Reader, length int) (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	raw := make([]byte, length)
	if _, err := io.ReadFull(source, raw); err != nil {
		return "", fmt.Errorf("storage: 生成 DeviceCode: %w", err)
	}
	result := make([]byte, length)
	for i, value := range raw {
		result[i] = alphabet[int(value)%len(alphabet)]
	}
	return string(result), nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("storage: 创建目录: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".ctyun-helper-*")
	if err != nil {
		return fmt.Errorf("storage: 创建临时文件: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return fmt.Errorf("storage: 设置文件权限: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("storage: 写入临时文件: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("storage: 同步临时文件: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("storage: 关闭临时文件: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("storage: 替换文件: %w", err)
	}
	return nil
}
