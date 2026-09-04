package points

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/uvwt/CtyunHelper/internal/ctyun/auth"
)

const DefaultOrigin = "https://desk.ctyun.cn"

type ClientOptions struct {
	Origin string
}

type Client struct {
	auth   *auth.Client
	origin string
}

func NewClient(authClient *auth.Client, options ClientOptions) *Client {
	origin := strings.TrimRight(options.Origin, "/")
	if origin == "" {
		origin = DefaultOrigin
	}
	return &Client{auth: authClient, origin: origin}
}

type Task struct {
	Name            string `json:"taskDefName"`
	Status          int    `json:"status"`
	CurrentProgress int    `json:"currentProgress"`
}

type Balance struct {
	PointType   int `json:"pointType"`
	TotalPoints int `json:"totalPoints"`
}

type Desktop struct {
	DesktopID   int64  `json:"desktopId"`
	DesktopName string `json:"desktopName"`
	ObjectID    int64  `json:"objId"`
	ObjectName  string `json:"objName"`
}

func (d Desktop) ID() int64 {
	if d.DesktopID != 0 {
		return d.DesktopID
	}
	return d.ObjectID
}

func (d Desktop) Name() string {
	if d.DesktopName != "" {
		return d.DesktopName
	}
	return d.ObjectName
}

type ProductMall struct {
	Series []ProductSeries `json:"series"`
}

type ProductSeries struct {
	SKUs []ProductSKU `json:"sku"`
}

type ProductSKU struct {
	ProductID   int64  `json:"prodId"`
	ProductName string `json:"prodName"`
	ProductType string `json:"prodType"`
	CostPoints  int    `json:"costPoints"`
	Status      int    `json:"prodStatus"`
	EffectiveAt string `json:"effDate"`
	ExpiresAt   string `json:"expireDate"`
}

type OrderRequest struct {
	BusinessChannel string     `json:"busiChannel"`
	OrderType       int        `json:"orderType"`
	PointType       int        `json:"pointType"`
	Points          int        `json:"points"`
	SKUs            []OrderSKU `json:"sku"`
}

type OrderSKU struct {
	ExecutionOrder int         `json:"execSort"`
	ProductID      int64       `json:"prodId"`
	ProductType    string      `json:"prodType"`
	Attributes     []OrderAttr `json:"attrs"`
}

type OrderAttr struct {
	Key   string `json:"attrKey"`
	Value int64  `json:"attrVal"`
}

func (c *Client) Tasks(ctx context.Context) ([]Task, error) {
	return doJSON[[]Task](ctx, c, http.MethodGet, "/selforder/api/marketing/userPoints/getTaskList", nil, nil)
}

func (c *Client) Balances(ctx context.Context) ([]Balance, error) {
	return doJSON[[]Balance](ctx, c, http.MethodGet, "/selforder/api/marketing/userPoints/getUserPoints", nil, nil)
}

func (c *Client) Products(ctx context.Context) ([]ProductMall, error) {
	query := url.Values{"prodId": {"17000000"}, "prodCode": {"POINTS"}}
	return doJSON[[]ProductMall](ctx, c, http.MethodGet, "/selforder/api/selforder/prod/get", query, nil)
}

func (c *Client) Desktops(ctx context.Context) ([]Desktop, error) {
	type pageData struct {
		DesktopList           []Desktop `json:"desktopList"`
		DesktopPoolList       []Desktop `json:"desktopPoolList"`
		PreemptionDesktopList []Desktop `json:"preemptionDesktopList"`
	}
	page, err := doJSON[pageData](ctx, c, http.MethodPost, "/selforder/api/desktop/client/pageDesktop", nil, map[string]int{"getCnt": 30})
	if err != nil {
		return nil, err
	}
	result := make([]Desktop, 0, len(page.DesktopList)+len(page.DesktopPoolList)+len(page.PreemptionDesktopList))
	result = append(result, page.DesktopList...)
	result = append(result, page.DesktopPoolList...)
	result = append(result, page.PreemptionDesktopList...)
	return result, nil
}

func (c *Client) PlaceOrder(ctx context.Context, request OrderRequest) (json.RawMessage, error) {
	return doJSON[json.RawMessage](ctx, c, http.MethodPost, "/selforder/api/selforder/paas/placeOrder", nil, request)
}

func (c *Client) GeneralPoints(ctx context.Context) (int, error) {
	balances, err := c.Balances(ctx)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, balance := range balances {
		if balance.PointType == 1 {
			total += balance.TotalPoints
		}
	}
	return total, nil
}

func doJSON[T any](ctx context.Context, client *Client, method, path string, query url.Values, body any) (T, error) {
	var zero T
	headers, err := client.auth.PublicHeaders(path)
	if err != nil {
		return zero, err
	}
	headers.Set("Cache-Control", "no-cache")
	headers.Set("Pragma", "no-cache")
	headers.Set("Content-Type", "application/json")
	headers.Set("From", "App-web")
	headers.Set("x-lang", "zh-CN")

	endpoint := client.origin + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return zero, fmt.Errorf("points: 编码请求: %w", err)
		}
		payload = bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, payload)
	if err != nil {
		return zero, err
	}
	request.Header = headers
	response, err := client.auth.Do(request)
	if err != nil {
		return zero, fmt.Errorf("points: %s: %w", path, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return zero, fmt.Errorf("points: %s HTTP %d", path, response.StatusCode)
	}
	var envelope struct {
		Code    int    `json:"code"`
		Message string `json:"msg"`
		Data    T      `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return zero, fmt.Errorf("points: 解析 %s: %w", path, err)
	}
	if envelope.Code != 0 {
		return zero, fmt.Errorf("points: code=%s: %s", strconv.Itoa(envelope.Code), envelope.Message)
	}
	return envelope.Data, nil
}
