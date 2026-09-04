package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/uvwt/CtyunHelper/internal/storage"
)

type fakeStartupControl struct {
	enabled bool
	setErr  error
	calls   []bool
}

func (f *fakeStartupControl) Enabled() (bool, error) { return f.enabled, nil }
func (f *fakeStartupControl) SetEnabled(enabled bool) error {
	f.calls = append(f.calls, enabled)
	if f.setErr != nil {
		return f.setErr
	}
	f.enabled = enabled
	return nil
}

func newSettingsFixture(t *testing.T) (storage.Paths, *Model) {
	t.Helper()
	root := t.TempDir()
	paths := storage.Paths{ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data")}
	config := storage.DefaultConfig()
	if err := storage.SaveConfig(paths, config); err != nil {
		t.Fatal(err)
	}
	return paths, NewModel(State{AutomationPaused: false})
}

func TestSettingsSaveUpdatesConfigStartupAndModel(t *testing.T) {
	paths, model := newSettingsFixture(t)
	startup := &fakeStartupControl{}
	service := NewSettingsService(paths, startup, model)
	if err := service.Save(GeneralSettings{AutomationEnabled: false, StartOnLogin: true}); err != nil {
		t.Fatal(err)
	}
	config, err := storage.LoadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	if config.Automation.Enabled || !startup.enabled || !model.Snapshot().AutomationPaused {
		t.Fatalf("config=%#v startup=%v state=%#v", config.Automation, startup.enabled, model.Snapshot())
	}
	current, err := service.Current()
	if err != nil {
		t.Fatal(err)
	}
	if current.AutomationEnabled || !current.StartOnLogin {
		t.Fatalf("current = %#v", current)
	}
}

func TestSettingsStartupFailureLeavesConfigAndModelUnchanged(t *testing.T) {
	paths, model := newSettingsFixture(t)
	startup := &fakeStartupControl{setErr: errors.New("registry denied")}
	service := NewSettingsService(paths, startup, model)
	if err := service.Save(GeneralSettings{AutomationEnabled: false, StartOnLogin: true}); err == nil {
		t.Fatal("expected startup error")
	}
	config, err := storage.LoadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	if !config.Automation.Enabled || model.Snapshot().AutomationPaused || startup.enabled {
		t.Fatalf("partial settings update: config=%#v startup=%v state=%#v", config.Automation, startup.enabled, model.Snapshot())
	}
}

func TestSettingsConfigFailureRollsBackStartup(t *testing.T) {
	paths, model := newSettingsFixture(t)
	startup := &fakeStartupControl{}
	service := NewSettingsService(paths, startup, model)
	if err := os.Chmod(paths.ConfigDir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(paths.ConfigDir, 0o700)
	err := service.Save(GeneralSettings{AutomationEnabled: false, StartOnLogin: true})
	if err == nil {
		t.Skip("filesystem permits writes despite directory mode; cannot exercise rollback")
	}
	if startup.enabled || len(startup.calls) < 2 || startup.calls[0] != true || startup.calls[len(startup.calls)-1] != false {
		t.Fatalf("startup rollback calls=%v enabled=%v", startup.calls, startup.enabled)
	}
	if model.Snapshot().AutomationPaused {
		t.Fatalf("model changed after config failure: %#v", model.Snapshot())
	}
}
