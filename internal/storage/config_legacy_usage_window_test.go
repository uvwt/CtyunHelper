package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadLegacyConfigDefaultsUsagePointsWindowToDisabled(t *testing.T) {
	root := t.TempDir()
	paths := Paths{ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data")}
	if err := os.MkdirAll(paths.ConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{"account":"legacy","automation":{"enabled":true}}`)
	if err := os.WriteFile(filepath.Join(paths.ConfigDir, "config.json"), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	window := loaded.Automation.UsagePointsWindow
	if window.Enabled || window.Start != "04:00" || window.End != "07:00" {
		t.Fatalf("legacy usage-points window = %#v", window)
	}
}
