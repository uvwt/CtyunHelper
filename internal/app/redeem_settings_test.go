package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uvwt/CtyunHelper/internal/automation"
	"github.com/uvwt/CtyunHelper/internal/ctyun/auth"
	"github.com/uvwt/CtyunHelper/internal/ctyun/points"
	"github.com/uvwt/CtyunHelper/internal/storage"
)

type redeemSettingsFixture struct {
	service    *RedeemSettingsService
	tasks      *TaskAutomation
	job        *automation.RedeemJob
	model      *Model
	paths      storage.Paths
	requests   atomic.Int32
	placeCalls atomic.Int32
	server     *httptest.Server
}

func newRedeemSettingsFixture(t *testing.T, initial storage.RedeemConfig, redeemState automation.RedeemState) *redeemSettingsFixture {
	t.Helper()
	fixture := &redeemSettingsFixture{}
	fixture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.requests.Add(1)
		switch r.URL.Path {
		case "/selforder/api/desktop/client/pageDesktop":
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
				"desktopList": []map[string]any{{"desktopId": 42, "desktopName": "主云电脑"}},
			}})
		case "/selforder/api/selforder/prod/get":
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": []map[string]any{{
				"series": []map[string]any{{"sku": []map[string]any{
					{"prodId": 99, "prodName": "月卡", "prodType": "gift", "costPoints": 300, "prodStatus": 2},
					{"prodId": 100, "prodName": "季卡", "prodType": "gift", "costPoints": 700, "prodStatus": 2},
				}}},
			}}})
		case "/selforder/api/selforder/paas/placeOrder":
			fixture.placeCalls.Add(1)
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"ok": true}})
		default:
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": []any{}})
		}
	}))
	t.Cleanup(fixture.server.Close)

	root := t.TempDir()
	fixture.paths = storage.Paths{ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data")}
	config := storage.DefaultConfig()
	config.Account = "account"
	config.Redeem = initial
	if err := storage.SaveConfig(fixture.paths, config); err != nil {
		t.Fatal(err)
	}

	authClient := auth.NewClient(auth.DeviceIdentity{Code: "ctyun_fixed"}, auth.ClientOptions{
		Random: strings.NewReader(strings.Repeat("x", 4096)),
	})
	authClient.UseProfile(auth.Profile{UserID: 1, UserEID: "eid", TenantID: 2, SecretKey: "key", CommonLoginReqHeader: "common", BondedDevice: true})
	pointsClient := points.NewClient(authClient, points.ClientOptions{Origin: fixture.server.URL})
	fixture.model = NewModel(State{Account: "account", Connection: ConnectionOnline})
	guard := automation.NewGuard(automation.DefaultPolicy(), automation.SafetyState{}, automation.GuardOptions{Now: time.Now})
	aiJob := automation.NewAIJob(
		&appAIPoints{tasks: []points.Task{{Name: automation.AITaskName, Status: automation.TaskDone}}},
		&appAIChat{}, guard, "你好",
	)
	fixture.job = automation.NewRedeemJob(pointsClient, guard, redeemPlanFromConfig(initial), redeemState, automation.RedeemJobOptions{})
	var err error
	fixture.tasks, err = NewTaskAutomationWithOptions(fixture.model, TaskAutomationOptions{AIJob: aiJob, RedeemJob: fixture.job})
	if err != nil {
		t.Fatal(err)
	}
	fixture.service = NewRedeemSettingsService(fixture.paths, pointsClient, fixture.tasks, fixture.model)
	return fixture
}

func TestRedeemSettingsCatalogIsReadOnly(t *testing.T) {
	fixture := newRedeemSettingsFixture(t, storage.RedeemConfig{}, automation.RedeemState{})
	catalog, err := fixture.service.Catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Desktops) != 1 || len(catalog.Products) != 2 {
		t.Fatalf("catalog = %#v", catalog)
	}
	if fixture.placeCalls.Load() != 0 {
		t.Fatalf("catalog unexpectedly called placeOrder %d times", fixture.placeCalls.Load())
	}
}

func TestRedeemSettingsSaveUsesFreshServerProductAndUpdatesRuntime(t *testing.T) {
	fixture := newRedeemSettingsFixture(t, storage.RedeemConfig{}, automation.RedeemState{})
	err := fixture.service.Save(context.Background(), SaveRedeemSettingsRequest{
		Enabled: true, DesktopID: 42, ProductID: 99, ProductType: "gift",
		MaxRedeemTimes: 2, ScheduleType: automation.RedeemScheduleInterval, IntervalDays: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	config, err := storage.LoadConfig(fixture.paths)
	if err != nil {
		t.Fatal(err)
	}
	if !config.Redeem.Enabled || config.Redeem.Account != "account" || config.Redeem.ProductName != "月卡" || config.Redeem.CostPoints != 300 || config.Redeem.MaxRedeemTimes != 2 {
		t.Fatalf("saved redeem = %#v", config.Redeem)
	}
	if !fixture.job.Enabled() || fixture.job.Account() != "account" || !fixture.model.Snapshot().RedeemEnabled {
		t.Fatalf("runtime plan/model not updated: state=%#v", fixture.model.Snapshot())
	}
	if fixture.placeCalls.Load() != 0 {
		t.Fatalf("saving settings unexpectedly called placeOrder %d times", fixture.placeCalls.Load())
	}
}

func TestRedeemSettingsDisableWorksOfflineWithoutCatalog(t *testing.T) {
	initial := storage.RedeemConfig{
		Enabled: true, Account: "account", DesktopID: 42, ProductID: 99, ProductName: "月卡",
		ProductType: "gift", CostPoints: 300, ScheduleType: automation.RedeemScheduleDaily, IntervalDays: 1,
	}
	fixture := newRedeemSettingsFixture(t, initial, automation.RedeemState{})
	before := fixture.requests.Load()
	fixture.model.Update(func(state *State) { state.Connection = ConnectionAuth })
	if err := fixture.service.Save(context.Background(), SaveRedeemSettingsRequest{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if fixture.requests.Load() != before || fixture.placeCalls.Load() != 0 {
		t.Fatalf("disable performed network requests: total=%d before=%d place=%d", fixture.requests.Load(), before, fixture.placeCalls.Load())
	}
	config, err := storage.LoadConfig(fixture.paths)
	if err != nil {
		t.Fatal(err)
	}
	if config.Redeem.Enabled || fixture.job.Enabled() || fixture.model.Snapshot().RedeemEnabled {
		t.Fatalf("redeem still enabled: config=%#v state=%#v", config.Redeem, fixture.model.Snapshot())
	}
	if config.Redeem.ProductID != 99 || config.Redeem.CostPoints != 300 {
		t.Fatalf("disable should preserve selections: %#v", config.Redeem)
	}
}

func TestRedeemSettingsPendingBlocksChangingEnabledPlan(t *testing.T) {
	initial := storage.RedeemConfig{
		Enabled: true, Account: "account", DesktopID: 42, ProductID: 99, ProductName: "月卡",
		ProductType: "gift", CostPoints: 300, ScheduleType: automation.RedeemScheduleDaily, IntervalDays: 1,
	}
	fixture := newRedeemSettingsFixture(t, initial, automation.RedeemState{LastAttemptStatus: automation.RedeemAttemptPending})
	err := fixture.service.Save(context.Background(), SaveRedeemSettingsRequest{
		Enabled: true, DesktopID: 42, ProductID: 100, ProductType: "gift",
		ScheduleType: automation.RedeemScheduleDaily, IntervalDays: 1,
	})
	if err == nil {
		t.Fatal("expected pending safety block")
	}
	config, loadErr := storage.LoadConfig(fixture.paths)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if config.Redeem.ProductID != 99 || fixture.placeCalls.Load() != 0 {
		t.Fatalf("pending plan changed: config=%#v place=%d", config.Redeem, fixture.placeCalls.Load())
	}
}

func TestRedeemSettingsSaveFailureLeavesRuntimePlanUnchanged(t *testing.T) {
	initial := storage.RedeemConfig{
		Enabled: true, Account: "account", DesktopID: 42, ProductID: 99, ProductName: "月卡",
		ProductType: "gift", CostPoints: 300, ScheduleType: automation.RedeemScheduleDaily, IntervalDays: 1,
	}
	fixture := newRedeemSettingsFixture(t, initial, automation.RedeemState{})
	if err := os.Chmod(fixture.paths.ConfigDir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(fixture.paths.ConfigDir, 0o700)
	err := fixture.service.Save(context.Background(), SaveRedeemSettingsRequest{
		Enabled: true, DesktopID: 42, ProductID: 100, ProductType: "gift",
		ScheduleType: automation.RedeemScheduleDaily, IntervalDays: 1,
	})
	if err == nil {
		t.Skip("filesystem permits writes despite directory mode; cannot exercise SaveConfig failure")
	}
	if !fixture.job.Enabled() || fixture.job.Account() != "account" || fixture.placeCalls.Load() != 0 {
		t.Fatalf("runtime plan unexpectedly changed after save failure")
	}
	if got := fixture.model.Snapshot(); !got.RedeemEnabled {
		t.Fatalf("runtime model unexpectedly disabled: %#v", got)
	}
}
