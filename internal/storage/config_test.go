package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnsureWindowsDeviceCreatesOnceAndNeverRotatesCode(t *testing.T) {
	config := DefaultConfig()
	fixed := time.Unix(100, 0)
	changed, err := EnsureWindowsDevice(&config, strings.NewReader(strings.Repeat("A", 64)), func() time.Time { return fixed })
	if err != nil {
		t.Fatal(err)
	}
	if !changed || !strings.HasPrefix(config.Device.Code, "ctyun_") || len(config.Device.Code) != len("ctyun_")+32 {
		t.Fatalf("device = %#v", config.Device)
	}
	first := config.Device.Code
	changed, err = EnsureWindowsDevice(&config, strings.NewReader(strings.Repeat("B", 64)), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if changed || config.Device.Code != first {
		t.Fatalf("device code rotated: first=%q current=%q", first, config.Device.Code)
	}
}

func TestEnsureWindowsDeviceRejectsSilentTypeChange(t *testing.T) {
	config := DefaultConfig()
	config.Device = DeviceConfig{Code: "ctyun_existing", Type: "60", Name: "Web", Model: "web"}
	if _, err := EnsureWindowsDevice(&config, nil, nil); err == nil {
		t.Fatal("expected device type mismatch error")
	}
	if config.Device.Code != "ctyun_existing" {
		t.Fatal("existing code must not change")
	}
}

func TestConfigRoundTrip(t *testing.T) {
	root := t.TempDir()
	paths := Paths{ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data")}
	config := DefaultConfig()
	config.Account = "account"
	config.Device = DeviceConfig{Code: "ctyun_fixed", Name: "Windows", Model: "windows", Type: "25"}
	if err := SaveConfig(paths, config); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Account != "account" || loaded.Device.Code != "ctyun_fixed" || loaded.Safety.MaxAI != 2 {
		t.Fatalf("loaded = %#v", loaded)
	}
	info, err := os.Stat(filepath.Join(paths.ConfigDir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatal("config file is empty")
	}
}
