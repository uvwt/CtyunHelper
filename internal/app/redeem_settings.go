package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/uvwt/CtyunHelper/internal/automation"
	"github.com/uvwt/CtyunHelper/internal/ctyun/points"
	"github.com/uvwt/CtyunHelper/internal/storage"
)

// RedeemCatalog 是兑换设置窗口使用的只读目录。这里不暴露 points 的完整
// HTTP DTO，避免 UI 依赖商城响应的内部层级。
type RedeemCatalog struct {
	Desktops []RedeemDesktop
	Products []RedeemProduct
}

type RedeemDesktop struct {
	ID   int64
	Name string
}

type RedeemProduct struct {
	ID         int64
	Name       string
	Type       string
	CostPoints int
}

type RedeemSettingsView struct {
	Enabled        bool
	Account        string
	DesktopID      int64
	ProductID      int64
	ProductType    string
	MaxRedeemTimes int
	ScheduleType   string
	IntervalDays   int
	MonthlyDays    []int
	Pending        bool
	PendingDate    string
	PendingTimes   int
	PendingPoints  int
}

type SaveRedeemSettingsRequest struct {
	Enabled        bool
	DesktopID      int64
	ProductID      int64
	ProductType    string
	MaxRedeemTimes int
	ScheduleType   string
	IntervalDays   int
	MonthlyDays    []int
}

// RedeemSettingsService 协调“只读目录 -> 配置落盘 -> 运行时计划更新”。
// 真正消费积分仍只发生在 RedeemJob；设置服务永远不调用 placeOrder。
type RedeemSettingsService struct {
	paths  storage.Paths
	points *points.Client
	tasks  *TaskAutomation
	model  *Model
}

func NewRedeemSettingsService(paths storage.Paths, pointsClient *points.Client, tasks *TaskAutomation, model *Model) *RedeemSettingsService {
	return &RedeemSettingsService{paths: paths, points: pointsClient, tasks: tasks, model: model}
}

func (s *RedeemSettingsService) Current() (RedeemSettingsView, error) {
	if s == nil {
		return RedeemSettingsView{}, fmt.Errorf("app: 兑换设置服务未初始化")
	}
	config, err := storage.LoadConfig(s.paths)
	if err != nil {
		return RedeemSettingsView{}, err
	}
	view := redeemSettingsFromConfig(config.Redeem)
	if s.tasks != nil && s.tasks.redeemJob != nil {
		state := s.tasks.redeemJob.Snapshot()
		if state.LastAttemptStatus == automation.RedeemAttemptPending {
			view.Pending = true
			view.PendingDate = state.LastAttemptDate
			view.PendingTimes = state.LastRedeemTimes
			view.PendingPoints = state.LastPointsSpent
		}
	}
	return view, nil
}

// Catalog 只请求云电脑和商品目录，不读取余额、不占用 Safety 额度、更不会下单。
func (s *RedeemSettingsService) Catalog(ctx context.Context) (RedeemCatalog, error) {
	if s == nil || s.points == nil || s.tasks == nil || s.model == nil {
		return RedeemCatalog{}, fmt.Errorf("app: 兑换设置依赖未初始化")
	}
	s.tasks.activityMu.RLock()
	defer s.tasks.activityMu.RUnlock()
	state := s.model.Snapshot()
	if state.Account == "" || state.Connection == ConnectionAuth || state.Connection == ConnectionDeviceBind {
		return RedeemCatalog{}, fmt.Errorf("app: 请先完成登录和设备绑定")
	}
	catalog, err := s.loadCatalog(ctx)
	if err != nil {
		return RedeemCatalog{}, err
	}
	// 目录刷新同时补齐首页展示用的最新名称，但不改兑换计划、不写配置，
	// 因此依然是只读操作；旧配置没有 desktopName 时也能立即显示真实名称。
	s.publishConfiguredTarget(catalog)
	return catalog, nil
}

func (s *RedeemSettingsService) Save(ctx context.Context, request SaveRedeemSettingsRequest) error {
	if s == nil || s.tasks == nil || s.model == nil || s.tasks.redeemJob == nil {
		return fmt.Errorf("app: 兑换设置依赖未初始化")
	}
	if !s.tasks.activityMu.TryLock() {
		return fmt.Errorf("app: 自动任务正在运行，暂不能修改兑换设置")
	}
	defer s.tasks.activityMu.Unlock()

	config, err := storage.LoadConfig(s.paths)
	if err != nil {
		return err
	}

	// 关闭自动兑换是纯本地操作：即使网络不可用或已经退出账号，也必须能
	// 立即关闭；其余选择保留，方便用户以后重新启用时继续编辑。
	if !request.Enabled {
		config.Redeem.Enabled = false
		if err := storage.SaveConfig(s.paths, config); err != nil {
			return err
		}
		plan := redeemPlanFromConfig(config.Redeem)
		if err := s.tasks.redeemJob.UpdatePlan(plan); err != nil {
			return fmt.Errorf("app: 更新运行中兑换计划: %w", err)
		}
		s.tasks.UpdateAccount(s.model.Snapshot().Account)
		return nil
	}

	state := s.model.Snapshot()
	if state.Account == "" || state.Connection == ConnectionAuth || state.Connection == ConnectionDeviceBind {
		return fmt.Errorf("app: 请先完成登录和设备绑定")
	}
	catalog, err := s.loadCatalog(ctx)
	if err != nil {
		return err
	}
	desktop, ok := findRedeemDesktop(catalog.Desktops, request.DesktopID)
	if !ok {
		return fmt.Errorf("app: 选择的云电脑 %d 已不存在，请刷新目录", request.DesktopID)
	}
	product, ok := findRedeemProduct(catalog.Products, request.ProductID, request.ProductType)
	if !ok {
		return fmt.Errorf("app: 选择的兑换商品已不存在，请刷新目录")
	}

	plan := automation.RedeemPlan{
		Enabled:        true,
		Account:        state.Account,
		DesktopID:      desktop.ID,
		DesktopName:    desktop.Name,
		ProductID:      product.ID,
		ProductName:    product.Name,
		ProductType:    product.Type,
		CostPoints:     product.CostPoints,
		MaxRedeemTimes: request.MaxRedeemTimes,
		ScheduleType:   request.ScheduleType,
		IntervalDays:   request.IntervalDays,
		MonthlyDays:    append([]int(nil), request.MonthlyDays...),
	}
	if err := automation.ValidateRedeemPlan(plan); err != nil {
		return err
	}

	// pending 表示 placeOrder 是否扣分未知。此时允许“关闭”，但不允许换成
	// 另一份启用计划来绕过保护；用户应先人工核对上一笔兑换结果。
	if s.tasks.redeemJob.Snapshot().LastAttemptStatus == automation.RedeemAttemptPending && redeemIdentityChanged(config.Redeem, plan) {
		return fmt.Errorf("app: 上次兑换结果仍不确定，请先关闭自动兑换并人工确认后再修改计划")
	}

	config.Redeem = storage.RedeemConfig{
		Enabled:        true,
		Account:        plan.Account,
		DesktopID:      plan.DesktopID,
		DesktopName:    plan.DesktopName,
		ProductID:      plan.ProductID,
		ProductName:    plan.ProductName,
		ProductType:    plan.ProductType,
		CostPoints:     plan.CostPoints,
		MaxRedeemTimes: plan.MaxRedeemTimes,
		ScheduleType:   plan.ScheduleType,
		IntervalDays:   plan.IntervalDays,
		MonthlyDays:    append([]int(nil), plan.MonthlyDays...),
	}

	// 先持久化，再更新内存计划。SaveConfig 失败时运行中的计划完全不变；
	// UpdatePlan 已在上面用同一份 plan 验证过，因此落盘成功后不会出现半更新。
	if err := storage.SaveConfig(s.paths, config); err != nil {
		return err
	}
	if err := s.tasks.redeemJob.UpdatePlan(plan); err != nil {
		return fmt.Errorf("app: 更新运行中兑换计划: %w", err)
	}
	s.tasks.UpdateAccount(state.Account)
	return nil
}

func (s *RedeemSettingsService) ResolvePending(succeeded bool) error {
	if s == nil || s.tasks == nil || s.model == nil || s.tasks.redeemJob == nil {
		return fmt.Errorf("app: 兑换设置依赖未初始化")
	}
	if !s.tasks.activityMu.TryLock() {
		return fmt.Errorf("app: 自动任务正在运行，暂不能确认兑换结果")
	}
	defer s.tasks.activityMu.Unlock()
	state := s.model.Snapshot()
	if state.Account == "" || s.tasks.redeemJob.Account() != state.Account {
		return fmt.Errorf("app: 请切换回兑换计划所属账号后再确认结果")
	}
	if err := s.tasks.redeemJob.ResolvePending(succeeded); err != nil {
		return err
	}
	s.tasks.UpdateAccount(state.Account)
	return nil
}

func (s *RedeemSettingsService) publishConfiguredTarget(catalog RedeemCatalog) {
	if s == nil || s.tasks == nil || s.tasks.redeemJob == nil || s.model == nil {
		return
	}
	plan := s.tasks.redeemJob.PlanSnapshot()
	desktop, desktopFound := findRedeemDesktop(catalog.Desktops, plan.DesktopID)
	product, productFound := findRedeemProduct(catalog.Products, plan.ProductID, plan.ProductType)
	if !desktopFound && !productFound {
		return
	}
	s.model.Update(func(state *State) {
		if desktopFound {
			state.RedeemDesktopName = desktop.Name
		}
		if productFound {
			state.RedeemProductName = product.Name
			state.RedeemCostPoints = product.CostPoints
		}
	})
}

func (s *RedeemSettingsService) loadCatalog(ctx context.Context) (RedeemCatalog, error) {
	if s.points == nil {
		return RedeemCatalog{}, fmt.Errorf("app: 积分 Client 未初始化")
	}
	desktops, err := s.points.Desktops(ctx)
	if err != nil {
		return RedeemCatalog{}, fmt.Errorf("app: 加载可绑定云电脑: %w", err)
	}
	malls, err := s.points.Products(ctx)
	if err != nil {
		return RedeemCatalog{}, fmt.Errorf("app: 加载兑换商品: %w", err)
	}

	catalog := RedeemCatalog{Desktops: make([]RedeemDesktop, 0, len(desktops))}
	for _, desktop := range desktops {
		if desktop.ID() == 0 {
			continue
		}
		name := strings.TrimSpace(desktop.Name())
		if name == "" {
			name = fmt.Sprintf("云电脑 %d", desktop.ID())
		}
		catalog.Desktops = append(catalog.Desktops, RedeemDesktop{ID: desktop.ID(), Name: name})
	}
	for _, mall := range malls {
		for _, series := range mall.Series {
			for _, sku := range series.SKUs {
				if sku.ProductID == 0 || sku.CostPoints <= 0 || strings.TrimSpace(sku.ProductType) == "" {
					continue
				}
				// 与真正兑换前校验保持一致，只把当前服务端标记可兑换的状态
				// 展示给用户；生效/过期时间仍会在下单前再次验证。
				if sku.Status != 0 && sku.Status != 2 {
					continue
				}
				name := strings.TrimSpace(sku.ProductName)
				if name == "" {
					name = fmt.Sprintf("商品 %d", sku.ProductID)
				}
				catalog.Products = append(catalog.Products, RedeemProduct{
					ID: sku.ProductID, Name: name, Type: sku.ProductType, CostPoints: sku.CostPoints,
				})
			}
		}
	}
	return catalog, nil
}

func redeemSettingsFromConfig(config storage.RedeemConfig) RedeemSettingsView {
	return RedeemSettingsView{
		Enabled: config.Enabled, Account: config.Account, DesktopID: config.DesktopID,
		ProductID: config.ProductID, ProductType: config.ProductType,
		MaxRedeemTimes: config.MaxRedeemTimes, ScheduleType: config.ScheduleType,
		IntervalDays: config.IntervalDays, MonthlyDays: append([]int(nil), config.MonthlyDays...),
	}
}

func redeemPlanFromConfig(config storage.RedeemConfig) automation.RedeemPlan {
	return automation.RedeemPlan{
		Enabled: config.Enabled, Account: config.Account, DesktopID: config.DesktopID, DesktopName: config.DesktopName,
		ProductID: config.ProductID, ProductName: config.ProductName, ProductType: config.ProductType,
		CostPoints: config.CostPoints, MaxRedeemTimes: config.MaxRedeemTimes,
		ScheduleType: config.ScheduleType, IntervalDays: config.IntervalDays,
		MonthlyDays: append([]int(nil), config.MonthlyDays...),
	}
}

func findRedeemDesktop(items []RedeemDesktop, id int64) (RedeemDesktop, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return RedeemDesktop{}, false
}

func findRedeemProduct(items []RedeemProduct, id int64, productType string) (RedeemProduct, bool) {
	for _, item := range items {
		if item.ID == id && item.Type == productType {
			return item, true
		}
	}
	return RedeemProduct{}, false
}

func redeemIdentityChanged(config storage.RedeemConfig, plan automation.RedeemPlan) bool {
	return config.Account != plan.Account || config.DesktopID != plan.DesktopID || config.ProductID != plan.ProductID ||
		config.ProductType != plan.ProductType || config.CostPoints != plan.CostPoints
}
