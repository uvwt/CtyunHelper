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

func newSettingsFixture(t *testing.T) (storage.Paths, *Model, *PointsSessionPolicy) {
	t.Helper()
	root := t.TempDir()
	paths := storage.Paths{ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data")}
	config := storage.DefaultConfig()
	if err := storage.SaveConfig(paths, config); err != nil {
		t.Fatal(err)
	}
	policy, err := NewPointsSessionPolicy(UsagePointsWindow{
		Enabled: config.Automation.UsagePointsWindow.Enabled,
		Start:   config.Automation.UsagePointsWindow.Start,
		End:     config.Automation.UsagePointsWindow.End,
	})
	if err != nil {
		t.Fatal(err)
	}
	return paths, NewModel(State{AutomationPaused: false}), policy
}

func TestSettingsSaveUpdatesConfigStartupAndModel(t *testing.T) {
	paths, model, policy := newSettingsFixture(t)
	startup := &fakeStartupControl{}
	service := NewSettingsService(paths, startup, model, policy)
	if err := service.Save(GeneralSettings{
		AutomationEnabled: true, StartOnLogin: true,
		UsagePointsWindowEnabled: true, UsagePointsWindowStart: "23:30", UsagePointsWindowEnd: "06:15",
	}); err != nil {
		t.Fatal(err)
	}
	config, err := storage.LoadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	if !config.Automation.Enabled || !startup.enabled || model.Snapshot().AutomationPaused {
		t.Fatalf("config=%#v startup=%v state=%#v", config.Automation, startup.enabled, model.Snapshot())
	}
	if got := config.Automation.UsagePointsWindow; !got.Enabled || got.Start != "23:30" || got.End != "06:15" {
		t.Fatalf("usage points window = %#v", got)
	}
	if got := policy.Snapshot(); !got.Enabled || got.Start != "23:30" || got.End != "06:15" {
		t.Fatalf("policy window = %#v", got)
	}
	current, err := service.Current()
	if err != nil {
		t.Fatal(err)
	}
	if !current.AutomationEnabled || !current.StartOnLogin || !current.UsagePointsWindowEnabled || current.UsagePointsWindowStart != "23:30" || current.UsagePointsWindowEnd != "06:15" {
		t.Fatalf("current = %#v", current)
	}
}

func TestSettingsStartupFailureLeavesConfigAndModelUnchanged(t *testing.T) {
	paths, model, policy := newSettingsFixture(t)
	startup := &fakeStartupControl{setErr: errors.New("registry denied")}
	service := NewSettingsService(paths, startup, model, policy)
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
	paths, model, policy := newSettingsFixture(t)
	startup := &fakeStartupControl{}
	service := NewSettingsService(paths, startup, model, policy)
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
