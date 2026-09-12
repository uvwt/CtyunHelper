package storage

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const CredentialTarget = "CtyunHelper/account"

type Config struct {
	Account    string           `json:"account"`
	Device     DeviceConfig     `json:"device"`
	Automation AutomationConfig `json:"automation"`
	Redeem     RedeemConfig     `json:"redeem"`
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
	Enabled           bool                    `json:"enabled"`
	UsagePointsWindow UsagePointsWindowConfig `json:"usagePointsWindow"`
}

type UsagePointsWindowConfig struct {
	Enabled bool   `json:"enabled"`
	Start   string `json:"start"`
	End     string `json:"end"`
}

// RedeemConfig 只描述用户明确选择的兑换计划。默认关闭；程序不会根据
// 当前可用商品自动挑选奖励，避免升级/首次运行后意外消费积分。
type RedeemConfig struct {
	Enabled        bool   `json:"enabled"`
	Account        string `json:"account"`
	DesktopID      int64  `json:"desktopId"`
	DesktopName    string `json:"desktopName,omitempty"`
	ProductID      int64  `json:"productId"`
	ProductName    string `json:"productName"`
	ProductType    string `json:"productType"`
	CostPoints     int    `json:"costPoints"`
	MaxRedeemTimes int    `json:"maxRedeemTimes"`
	ScheduleType   string `json:"scheduleType"`
	IntervalDays   int    `json:"intervalDays"`
	MonthlyDays    []int  `json:"monthlyDays"`
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
		Automation: AutomationConfig{
			Enabled: true,
			UsagePointsWindow: UsagePointsWindowConfig{
				Enabled: false,
				Start:   "04:00",
				End:     "07:00",
			},
		},
		Redeem: RedeemConfig{
			Enabled: false, ScheduleType: "daily", IntervalDays: 1,
		},
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

// configMu 在进程内串行化 config.json 的读-改-写。LoadConfig/SaveConfig 各自
// 无锁，调用方各自 Load→改→Save 会互相覆盖（例如登录提交与设置保存并发），
// 统一改走 UpdateConfig 即可消除丢失更新。
var configMu sync.Mutex

// UpdateConfig 在进程内互斥下执行 Load → mutate → Save 的原子配置更新。
// mutate 返回错误时不写盘、配置保持原值。调用方需要拿到更新后的完整配置时，
// 可在 mutate 内复制 *Config 到闭包外变量。
// 注意锁序：调用方如已持有 activityMu 等业务锁，必须先于 configMu 获取，
// 本函数绝不反向调用业务层。
func UpdateConfig(paths Paths, mutate func(*Config) error) error {
	configMu.Lock()
	defer configMu.Unlock()
	config, err := LoadConfig(paths)
	if err != nil {
		return err
	}
	if err := mutate(&config); err != nil {
		return err
	}
	return SaveConfig(paths, config)
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
	// 248 = 256 - 256%62：拒绝最高位区间，消除 value%len(alphabet) 的模偏差。
	const unbiasedMax = 248
	result := make([]byte, length)
	raw := make([]byte, 1)
	for i := 0; i < length; i++ {
		for {
			if _, err := io.ReadFull(source, raw); err != nil {
				return "", fmt.Errorf("storage: 生成 DeviceCode: %w", err)
			}
			if int(raw[0]) < unbiasedMax {
				break
			}
		}
		result[i] = alphabet[int(raw[0])%len(alphabet)]
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
