package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/uvwt/CtyunHelper/internal/ctyun/points"
)

const (
	RedeemScheduleDaily       = "daily"
	RedeemScheduleInterval    = "interval_days"
	RedeemScheduleMonthlyDays = "monthly_days"

	RedeemAttemptPending = "pending"
	RedeemAttemptFailed  = "failed"
	RedeemAttemptSuccess = "success"
)

type RedeemPlan struct {
	Enabled        bool
	Account        string
	DesktopID      int64
	ProductID      int64
	ProductName    string
	ProductType    string
	CostPoints     int
	MaxRedeemTimes int
	ScheduleType   string
	IntervalDays   int
	MonthlyDays    []int
}

type RedeemState struct {
	LastAttemptDate   string `json:"lastAttemptDate"`
	LastAttemptStatus string `json:"lastAttemptStatus"`
	LastSuccessDate   string `json:"lastSuccessDate"`
	LastRedeemTimes   int    `json:"lastRedeemTimes"`
	LastPointsSpent   int    `json:"lastPointsSpent"`
}

type RedeemResult struct {
	Redeemed      bool
	SkippedReason string
	Times         int
	PointsSpent   int
	PointsBefore  int
}

type redeemClient interface {
	GeneralPoints(context.Context) (int, error)
	Products(context.Context) ([]points.ProductMall, error)
	Desktops(context.Context) ([]points.Desktop, error)
	PlaceOrder(context.Context, points.OrderRequest) (json.RawMessage, error)
}

type RedeemJobOptions struct {
	Now       func() time.Time
	SaveState func(RedeemState) error
}

type RedeemJob struct {
	client redeemClient
	guard  *Guard
	plan   RedeemPlan
	now    func() time.Time
	save   func(RedeemState) error

	mu    sync.Mutex
	state RedeemState
}

func NewRedeemJob(client redeemClient, guard *Guard, plan RedeemPlan, initial RedeemState, options RedeemJobOptions) *RedeemJob {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	plan.MonthlyDays = append([]int(nil), plan.MonthlyDays...)
	return &RedeemJob{client: client, guard: guard, plan: plan, state: initial, now: now, save: options.SaveState}
}

func (j *RedeemJob) Enabled() bool {
	if j == nil {
		return false
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.plan.Enabled
}

func (j *RedeemJob) Account() string {
	if j == nil {
		return ""
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return strings.TrimSpace(j.plan.Account)
}

func (j *RedeemJob) Validate() error {
	if j == nil {
		return fmt.Errorf("automation: 兑换 Job 未初始化")
	}
	j.mu.Lock()
	plan := cloneRedeemPlan(j.plan)
	j.mu.Unlock()
	return ValidateRedeemPlan(plan)
}

// UpdatePlan 只替换后续运行使用的配置，不修改兑换历史状态。这样用户在
// 当天成功兑换后修改商品/次数，也不会通过“改配置”绕过当日一次限制；
// pending 的不确定结果同样继续阻止后续自动兑换。
func (j *RedeemJob) UpdatePlan(plan RedeemPlan) error {
	if j == nil {
		return fmt.Errorf("automation: 兑换 Job 未初始化")
	}
	if err := ValidateRedeemPlan(plan); err != nil {
		return err
	}
	j.mu.Lock()
	j.plan = cloneRedeemPlan(plan)
	j.mu.Unlock()
	return nil
}

func (j *RedeemJob) Snapshot() RedeemState {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.state
}

// ResolvePending 只用于用户已经人工核对服务端结果后的显式恢复。
// succeeded=true 会把原尝试日记为成功，避免当天再次兑换；false 仍保留
// LastAttemptDate，因此同一天也不会再次下单，最早下一自然日才可重试。
func (j *RedeemJob) ResolvePending(succeeded bool) error {
	if j == nil {
		return fmt.Errorf("automation: 兑换 Job 未初始化")
	}
	j.mu.Lock()
	if j.state.LastAttemptStatus != RedeemAttemptPending {
		j.mu.Unlock()
		return fmt.Errorf("automation: 当前没有待人工确认的兑换")
	}
	resolved := j.state
	if succeeded {
		resolved.LastAttemptStatus = RedeemAttemptSuccess
		resolved.LastSuccessDate = resolved.LastAttemptDate
	} else {
		resolved.LastAttemptStatus = RedeemAttemptFailed
	}
	j.mu.Unlock()

	// 人工确认属于恢复动作，必须先确保磁盘状态写成功再改变进程内状态。
	// 否则写盘失败后当前进程会认为已解除 pending，而重启又恢复成 pending。
	if j.save != nil {
		if err := j.save(resolved); err != nil {
			return fmt.Errorf("automation: 保存人工确认结果: %w", err)
		}
	}
	j.mu.Lock()
	j.state = resolved
	j.mu.Unlock()
	return nil
}

func (j *RedeemJob) Run(ctx context.Context) (RedeemResult, error) {
	if j == nil || j.client == nil || j.guard == nil {
		return RedeemResult{}, fmt.Errorf("automation: 兑换 Job 依赖未初始化")
	}
	j.mu.Lock()
	plan := cloneRedeemPlan(j.plan)
	state := j.state
	j.mu.Unlock()
	if !plan.Enabled {
		return RedeemResult{SkippedReason: "自动兑换未启用"}, nil
	}
	if err := ValidateRedeemPlan(plan); err != nil {
		return RedeemResult{}, err
	}
	today := j.now().Local()
	allowed, reason, err := ShouldRedeemToday(plan, state, today)
	if err != nil {
		return RedeemResult{}, err
	}
	if !allowed {
		return RedeemResult{SkippedReason: reason}, nil
	}

	currentPoints, err := j.client.GeneralPoints(ctx)
	if err != nil {
		return RedeemResult{}, fmt.Errorf("automation: 兑换前刷新通用积分: %w", err)
	}
	result := RedeemResult{PointsBefore: currentPoints}
	if currentPoints < plan.CostPoints {
		result.SkippedReason = fmt.Sprintf("当前积分 %d，不足 %d", currentPoints, plan.CostPoints)
		return result, nil
	}

	if err := j.verifyDesktop(ctx, plan); err != nil {
		return result, err
	}
	if err := j.verifyProduct(ctx, plan, today); err != nil {
		return result, err
	}

	times := currentPoints / plan.CostPoints
	if plan.MaxRedeemTimes > 0 && times > plan.MaxRedeemTimes {
		times = plan.MaxRedeemTimes
	}
	if times <= 0 {
		result.SkippedReason = "积分不足"
		return result, nil
	}
	result.Times = times
	result.PointsSpent = times * plan.CostPoints

	// 每日高价值额度必须先持久化成功；失败时绝不能发送 placeOrder。
	if err := j.guard.Claim(ActionRedeem); err != nil {
		return result, fmt.Errorf("automation: 兑换被保守策略阻止: %w", err)
	}

	// 在真正下单前先持久化 pending。若进程在 HTTP 返回前崩溃，下次启动会
	// 将其视为“结果不确定”并停止自动兑换，而不是猜测失败后再次扣积分。
	pending := state
	pending.LastAttemptDate = today.Format("2006-01-02")
	pending.LastAttemptStatus = RedeemAttemptPending
	pending.LastRedeemTimes = times
	pending.LastPointsSpent = result.PointsSpent
	if err := j.commitState(pending); err != nil {
		return result, fmt.Errorf("automation: 保存兑换 pending 状态: %w", err)
	}

	request := BuildOrderRequest(plan, times)
	if _, err := j.client.PlaceOrder(ctx, request); err != nil {
		failed := pending
		failed.LastAttemptStatus = RedeemAttemptFailed
		stateErr := j.commitState(failed)
		safetyErr := j.guard.RecordFailure()
		return result, errors.Join(fmt.Errorf("automation: 兑换请求失败: %w", err), stateErr, safetyErr)
	}

	succeeded := pending
	succeeded.LastAttemptStatus = RedeemAttemptSuccess
	succeeded.LastSuccessDate = pending.LastAttemptDate
	succeeded.LastRedeemTimes = times
	succeeded.LastPointsSpent = result.PointsSpent
	stateErr := j.commitState(succeeded)
	safetyErr := j.guard.RecordSuccess()
	if stateErr != nil || safetyErr != nil {
		// placeOrder 已成功，必须把结果标记为成功返回给 UI，错误只表示本地状态
		// 未完全收尾；当天 Safety Claim 仍会阻止再次下单。
		result.Redeemed = true
		return result, errors.Join(stateErr, safetyErr)
	}
	result.Redeemed = true
	return result, nil
}

func ValidateRedeemPlan(plan RedeemPlan) error {
	if !plan.Enabled {
		return nil
	}
	if strings.TrimSpace(plan.Account) == "" || plan.DesktopID <= 0 || plan.ProductID <= 0 || strings.TrimSpace(plan.ProductType) == "" || plan.CostPoints <= 0 {
		return fmt.Errorf("automation: 兑换配置缺少 account/desktop/product/type/cost")
	}
	if plan.MaxRedeemTimes < 0 {
		return fmt.Errorf("automation: maxRedeemTimes 不能小于 0")
	}
	switch plan.ScheduleType {
	case "", RedeemScheduleDaily:
	case RedeemScheduleInterval:
		if plan.IntervalDays <= 0 {
			return fmt.Errorf("automation: intervalDays 必须大于 0")
		}
	case RedeemScheduleMonthlyDays:
		if len(plan.MonthlyDays) == 0 {
			return fmt.Errorf("automation: monthlyDays 不能为空")
		}
		for _, day := range plan.MonthlyDays {
			if day != -1 && (day < 1 || day > 31) {
				return fmt.Errorf("automation: monthlyDays 包含无效日期 %d", day)
			}
		}
	default:
		return fmt.Errorf("automation: 未知兑换计划 %q", plan.ScheduleType)
	}
	return nil
}

func ShouldRedeemToday(plan RedeemPlan, state RedeemState, now time.Time) (bool, string, error) {
	if !plan.Enabled {
		return false, "自动兑换未启用", nil
	}
	if err := ValidateRedeemPlan(plan); err != nil {
		return false, "", err
	}
	today := now.Format("2006-01-02")
	if state.LastAttemptStatus == RedeemAttemptPending {
		return false, "上次兑换结果不确定，已停止自动兑换", nil
	}
	if state.LastSuccessDate == today {
		return false, "今天已经兑换成功", nil
	}
	if state.LastAttemptDate == today {
		return false, "今天已经发起过兑换请求", nil
	}

	schedule := plan.ScheduleType
	if schedule == "" {
		schedule = RedeemScheduleDaily
	}
	switch schedule {
	case RedeemScheduleDaily:
		return true, "每日兑换策略", nil
	case RedeemScheduleInterval:
		if state.LastSuccessDate == "" {
			return true, "间隔兑换首次执行", nil
		}
		last, err := time.ParseInLocation("2006-01-02", state.LastSuccessDate, now.Location())
		if err != nil {
			return false, "", fmt.Errorf("automation: 上次兑换日期无效: %w", err)
		}
		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		nextAllowed := last.AddDate(0, 0, plan.IntervalDays)
		passed := 0
		for cursor := last; cursor.Before(todayStart); cursor = cursor.AddDate(0, 0, 1) {
			passed++
		}
		return !todayStart.Before(nextAllowed), fmt.Sprintf("距上次兑换 %d 天，要求 %d 天", passed, plan.IntervalDays), nil
	case RedeemScheduleMonthlyDays:
		lastDay := time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, now.Location()).Day()
		for _, day := range plan.MonthlyDays {
			if day == now.Day() || (day == -1 && now.Day() == lastDay) {
				return true, "命中每月兑换日", nil
			}
		}
		return false, "今天不是配置的每月兑换日", nil
	default:
		return false, "", fmt.Errorf("automation: 未知兑换计划 %q", schedule)
	}
}

func BuildOrderRequest(plan RedeemPlan, times int) points.OrderRequest {
	skus := make([]points.OrderSKU, 0, times)
	for index := 0; index < times; index++ {
		skus = append(skus, points.OrderSKU{
			ExecutionOrder: index + 1,
			ProductID:      plan.ProductID,
			ProductType:    plan.ProductType,
			Attributes: []points.OrderAttr{{
				Key: "bindDesktopId", Value: plan.DesktopID,
			}},
		})
	}
	return points.OrderRequest{
		BusinessChannel: "010",
		OrderType:       1,
		PointType:       1,
		Points:          plan.CostPoints * times,
		SKUs:            skus,
	}
}

func (j *RedeemJob) verifyDesktop(ctx context.Context, plan RedeemPlan) error {
	desktops, err := j.client.Desktops(ctx)
	if err != nil {
		return fmt.Errorf("automation: 兑换前验证云电脑: %w", err)
	}
	for _, desktop := range desktops {
		if desktop.ID() == plan.DesktopID {
			return nil
		}
	}
	return fmt.Errorf("automation: 配置的云电脑 %d 已不存在", plan.DesktopID)
}

func (j *RedeemJob) verifyProduct(ctx context.Context, plan RedeemPlan, now time.Time) error {
	malls, err := j.client.Products(ctx)
	if err != nil {
		return fmt.Errorf("automation: 兑换前验证商品: %w", err)
	}
	for _, mall := range malls {
		for _, series := range mall.Series {
			for _, sku := range series.SKUs {
				if sku.ProductID != plan.ProductID || sku.ProductType != plan.ProductType {
					continue
				}
				if sku.Status != 0 && sku.Status != 2 {
					return fmt.Errorf("automation: 配置商品当前不可兑换，status=%d", sku.Status)
				}
				if sku.CostPoints != plan.CostPoints {
					return fmt.Errorf("automation: 商品积分成本已从 %d 变为 %d，需重新确认配置", plan.CostPoints, sku.CostPoints)
				}
				if active, reason := productActiveAt(sku, now); !active {
					return fmt.Errorf("automation: 配置商品当前不可兑换: %s", reason)
				}
				return nil
			}
		}
	}
	return fmt.Errorf("automation: 配置的商品 %d 已不存在", plan.ProductID)
}

func cloneRedeemPlan(plan RedeemPlan) RedeemPlan {
	result := plan
	result.MonthlyDays = append([]int(nil), plan.MonthlyDays...)
	return result
}

func productActiveAt(sku points.ProductSKU, now time.Time) (bool, string) {
	if value, ok := parseProductTime(sku.EffectiveAt, now.Location()); ok && now.Before(value) {
		return false, "尚未生效"
	}
	if value, ok := parseProductTime(sku.ExpiresAt, now.Location()); ok && now.After(value) {
		return false, "已经过期"
	}
	return true, ""
}

func parseProductTime(value string, location *time.Location) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		parsed, err := time.ParseInLocation(layout, value, location)
		if err == nil {
			return parsed, true
		}
	}
	// 旧脚本遇到无法解析的活动时间会忽略该字段；这里保持兼容，避免因为
	// 服务端日期格式微调把仍可兑换商品误判为配置损坏。
	return time.Time{}, false
}

func (j *RedeemJob) commitState(state RedeemState) error {
	j.mu.Lock()
	j.state = state
	j.mu.Unlock()
	if j.save == nil {
		return nil
	}
	return j.save(state)
}
