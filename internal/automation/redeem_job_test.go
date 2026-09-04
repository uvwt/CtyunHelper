package automation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/uvwt/CtyunHelper/internal/ctyun/points"
)

type fakeRedeemClient struct {
	points      int
	pointsErr   error
	products    []points.ProductMall
	productsErr error
	desktops    []points.Desktop
	desktopsErr error
	placeCalls  int
	requests    []points.OrderRequest
	placeErr    error
}

func (f *fakeRedeemClient) GeneralPoints(context.Context) (int, error) { return f.points, f.pointsErr }
func (f *fakeRedeemClient) Products(context.Context) ([]points.ProductMall, error) {
	return f.products, f.productsErr
}
func (f *fakeRedeemClient) Desktops(context.Context) ([]points.Desktop, error) {
	return f.desktops, f.desktopsErr
}
func (f *fakeRedeemClient) PlaceOrder(_ context.Context, request points.OrderRequest) (json.RawMessage, error) {
	f.placeCalls++
	f.requests = append(f.requests, request)
	if f.placeErr != nil {
		return nil, f.placeErr
	}
	return json.RawMessage(`{"ok":true}`), nil
}

func validRedeemPlan() RedeemPlan {
	return RedeemPlan{
		Enabled: true, Account: "account", DesktopID: 42, ProductID: 99, ProductName: "奖励",
		ProductType: "gift", CostPoints: 300, MaxRedeemTimes: 0,
		ScheduleType: RedeemScheduleDaily, IntervalDays: 1,
	}
}

func validRedeemClient() *fakeRedeemClient {
	return &fakeRedeemClient{
		points:   650,
		desktops: []points.Desktop{{DesktopID: 42, DesktopName: "云电脑"}},
		products: []points.ProductMall{{Series: []points.ProductSeries{{SKUs: []points.ProductSKU{{
			ProductID: 99, ProductName: "奖励", ProductType: "gift", CostPoints: 300, Status: 2,
		}}}}}},
	}
}

func TestBuildOrderRequestMatchesLegacyPayload(t *testing.T) {
	request := BuildOrderRequest(validRedeemPlan(), 2)
	if request.BusinessChannel != "010" || request.OrderType != 1 || request.PointType != 1 || request.Points != 600 || len(request.SKUs) != 2 {
		t.Fatalf("request = %#v", request)
	}
	for index, sku := range request.SKUs {
		if sku.ExecutionOrder != index+1 || sku.ProductID != 99 || sku.ProductType != "gift" || len(sku.Attributes) != 1 || sku.Attributes[0].Key != "bindDesktopId" || sku.Attributes[0].Value != 42 {
			t.Fatalf("sku[%d] = %#v", index, sku)
		}
	}
}

func TestRedeemScheduleSupportsIntervalAndMonthEnd(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	plan := validRedeemPlan()
	plan.ScheduleType = RedeemScheduleInterval
	plan.IntervalDays = 3
	state := RedeemState{LastSuccessDate: "2026-09-01", LastAttemptDate: "2026-09-01", LastAttemptStatus: RedeemAttemptSuccess}
	allowed, _, err := ShouldRedeemToday(plan, state, time.Date(2026, 9, 3, 10, 0, 0, 0, location))
	if err != nil || allowed {
		t.Fatalf("day 2 allowed=%v err=%v", allowed, err)
	}
	allowed, _, err = ShouldRedeemToday(plan, state, time.Date(2026, 9, 4, 10, 0, 0, 0, location))
	if err != nil || !allowed {
		t.Fatalf("day 3 allowed=%v err=%v", allowed, err)
	}

	plan.ScheduleType = RedeemScheduleMonthlyDays
	plan.MonthlyDays = []int{1, -1}
	state = RedeemState{}
	allowed, _, err = ShouldRedeemToday(plan, state, time.Date(2026, 9, 30, 10, 0, 0, 0, location))
	if err != nil || !allowed {
		t.Fatalf("month end allowed=%v err=%v", allowed, err)
	}
}

func TestRedeemSavesPendingBeforeSingleOrderAndSuccess(t *testing.T) {
	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.Local)
	client := validRedeemClient()
	guard := NewGuard(DefaultPolicy(), SafetyState{}, GuardOptions{Now: func() time.Time { return now }})
	var saved []RedeemState
	job := NewRedeemJob(client, guard, validRedeemPlan(), RedeemState{}, RedeemJobOptions{
		Now: func() time.Time { return now },
		SaveState: func(state RedeemState) error {
			saved = append(saved, state)
			return nil
		},
	})
	result, err := job.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Redeemed || result.Times != 2 || result.PointsSpent != 600 || client.placeCalls != 1 {
		t.Fatalf("result=%#v calls=%d", result, client.placeCalls)
	}
	if len(saved) != 2 || saved[0].LastAttemptStatus != RedeemAttemptPending || saved[1].LastAttemptStatus != RedeemAttemptSuccess || saved[1].LastSuccessDate != "2026-09-04" {
		t.Fatalf("saved states = %#v", saved)
	}
	if guard.Snapshot().DailyActions[ActionRedeem] != 1 {
		t.Fatalf("safety = %#v", guard.Snapshot())
	}
	if got := client.requests[0]; got.Points != 600 || len(got.SKUs) != 2 {
		t.Fatalf("order = %#v", got)
	}
}

func TestRedeemNeverOrdersWhenSafetyPersistenceFails(t *testing.T) {
	client := validRedeemClient()
	guard := NewGuard(DefaultPolicy(), SafetyState{}, GuardOptions{
		Now:  time.Now,
		Save: func(SafetyState) error { return errors.New("disk failed") },
	})
	job := NewRedeemJob(client, guard, validRedeemPlan(), RedeemState{}, RedeemJobOptions{})
	if _, err := job.Run(context.Background()); err == nil {
		t.Fatal("expected safety persistence error")
	}
	if client.placeCalls != 0 {
		t.Fatalf("placeOrder calls = %d, want 0", client.placeCalls)
	}
}

func TestRedeemFailureDoesNotRetryOrProbeSmallerQuantity(t *testing.T) {
	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.Local)
	client := validRedeemClient()
	client.placeErr = errors.New("server rejected")
	guard := NewGuard(DefaultPolicy(), SafetyState{}, GuardOptions{Now: func() time.Time { return now }})
	job := NewRedeemJob(client, guard, validRedeemPlan(), RedeemState{}, RedeemJobOptions{Now: func() time.Time { return now }})
	if _, err := job.Run(context.Background()); err == nil {
		t.Fatal("expected order failure")
	}
	if client.placeCalls != 1 {
		t.Fatalf("placeOrder calls = %d, want exactly 1", client.placeCalls)
	}
	result, err := job.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if client.placeCalls != 1 || result.SkippedReason == "" {
		t.Fatalf("second run result=%#v calls=%d", result, client.placeCalls)
	}
	if job.Snapshot().LastAttemptStatus != RedeemAttemptFailed {
		t.Fatalf("state = %#v", job.Snapshot())
	}
}

func TestPendingRedeemBlocksAutomaticRetryAfterCrash(t *testing.T) {
	client := validRedeemClient()
	guard := NewGuard(DefaultPolicy(), SafetyState{}, GuardOptions{Now: time.Now})
	job := NewRedeemJob(client, guard, validRedeemPlan(), RedeemState{
		LastAttemptDate: "2026-09-03", LastAttemptStatus: RedeemAttemptPending,
	}, RedeemJobOptions{})
	result, err := job.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if client.placeCalls != 0 || result.SkippedReason == "" {
		t.Fatalf("result=%#v calls=%d", result, client.placeCalls)
	}
}

func TestChangedProductCostRequiresExplicitReconfiguration(t *testing.T) {
	client := validRedeemClient()
	client.products[0].Series[0].SKUs[0].CostPoints = 500
	guard := NewGuard(DefaultPolicy(), SafetyState{}, GuardOptions{Now: time.Now})
	job := NewRedeemJob(client, guard, validRedeemPlan(), RedeemState{}, RedeemJobOptions{})
	if _, err := job.Run(context.Background()); err == nil {
		t.Fatal("expected product cost mismatch")
	}
	if client.placeCalls != 0 || guard.Snapshot().DailyActions[ActionRedeem] != 0 {
		t.Fatalf("calls=%d safety=%#v", client.placeCalls, guard.Snapshot())
	}
}
