package storage

import "time"

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
