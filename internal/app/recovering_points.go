package app

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/uvwt/CtyunHelper/internal/ctyun/auth"
	"github.com/uvwt/CtyunHelper/internal/ctyun/points"
)

// RecoveringPointsClient 在只读 Points 请求确认主认证 40010 后，先恢复同账号
// Profile，再仅重试当前 HTTP 级操作一次。它不会重跑整个 AI/兑换 Job，避免
// 已执行的业务步骤被重复；PlaceOrder 也明确不在这里自动重试。
type RecoveringPointsClient struct {
	client *points.Client
	flow   *AuthFlow
}

func NewRecoveringPointsClient(client *points.Client, flow *AuthFlow) *RecoveringPointsClient {
	return &RecoveringPointsClient{client: client, flow: flow}
}

func (c *RecoveringPointsClient) Tasks(ctx context.Context) ([]points.Task, error) {
	revision := c.flow.currentProfileRevision()
	result, err := c.client.Tasks(ctx)
	if retry, recoveryErr := c.recoverIfExpired(ctx, revision, err); !retry {
		return result, recoveryErr
	}
	return c.client.Tasks(ctx)
}

func (c *RecoveringPointsClient) GeneralPoints(ctx context.Context) (int, error) {
	revision := c.flow.currentProfileRevision()
	result, err := c.client.GeneralPoints(ctx)
	if retry, recoveryErr := c.recoverIfExpired(ctx, revision, err); !retry {
		return result, recoveryErr
	}
	return c.client.GeneralPoints(ctx)
}

func (c *RecoveringPointsClient) Products(ctx context.Context) ([]points.ProductMall, error) {
	revision := c.flow.currentProfileRevision()
	result, err := c.client.Products(ctx)
	if retry, recoveryErr := c.recoverIfExpired(ctx, revision, err); !retry {
		return result, recoveryErr
	}
	return c.client.Products(ctx)
}

func (c *RecoveringPointsClient) Desktops(ctx context.Context) ([]points.Desktop, error) {
	revision := c.flow.currentProfileRevision()
	result, err := c.client.Desktops(ctx)
	if retry, recoveryErr := c.recoverIfExpired(ctx, revision, err); !retry {
		return result, recoveryErr
	}
	return c.client.Desktops(ctx)
}

func (c *RecoveringPointsClient) PlaceOrder(ctx context.Context, request points.OrderRequest) (json.RawMessage, error) {
	// 下单属于有副作用操作。即使返回 40010，也交给上层已有的 pending/失败
	// 保护处理，不能在客户端层猜测服务端是否已受理后自动重复提交。
	return c.client.PlaceOrder(ctx, request)
}

func (c *RecoveringPointsClient) recoverIfExpired(ctx context.Context, revision uint64, requestErr error) (bool, error) {
	if requestErr == nil {
		return false, nil
	}
	if !auth.RequiresAuthentication(requestErr) {
		return false, requestErr
	}
	if err := c.flow.RecoverExpiredProfile(ctx, revision); err != nil {
		return false, errors.Join(requestErr, err)
	}
	return true, nil
}
