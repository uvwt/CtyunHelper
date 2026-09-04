package desktop

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

const (
	pageDesktopPath      = "/api/desktop/client/pageDesktop"
	queryConnectDataPath = "/api/desktop/client/queryConnectData"
	connectPath          = "/api/desktop/client/connect"
)

type ClientOptions struct {
	MasterOrigin string
}

type Client struct {
	auth         *auth.Client
	masterOrigin string
}

func NewClient(authClient *auth.Client, options ClientOptions) *Client {
	origin := strings.TrimRight(options.MasterOrigin, "/")
	if origin == "" {
		origin = authClient.APIOrigin()
	}
	return &Client{auth: authClient, masterOrigin: origin}
}

// List 按当前官方客户端请求形状读取普通云电脑列表。请求体保持稳定、明确，
// 不把页面筛选或 UI 状态渗入协议层。
func (c *Client) List(ctx context.Context) ([]Desktop, error) {
	body := struct {
		DesktopTypes []string `json:"desktopTypes"`
		Count        int      `json:"getCnt"`
		SortType     string   `json:"sortType"`
	}{
		DesktopTypes: []string{"1", "2001", "2002"},
		Count:        30,
		SortType:     "createTimeV1",
	}
	type pageData struct {
		DesktopList           []Desktop `json:"desktopList"`
		DesktopPoolList       []Desktop `json:"desktopPoolList"`
		PreemptionDesktopList []Desktop `json:"preemptionDesktopList"`
	}
	page, err := doJSON[pageData](ctx, c, c.masterOrigin, http.MethodPost, pageDesktopPath, nil, body)
	if err != nil {
		return nil, err
	}
	result := make([]Desktop, 0, len(page.DesktopList)+len(page.DesktopPoolList)+len(page.PreemptionDesktopList))
	result = append(result, page.DesktopList...)
	result = append(result, page.DesktopPoolList...)
	result = append(result, page.PreemptionDesktopList...)
	return result, nil
}

// ResolveConnection 完整执行当前官方客户端连接前的 queryConnectData -> connect 链。
// 两个接口都使用云电脑所属区域 Host 的 serverNode 签名；返回 connect 的最终连接数据。
func (c *Client) ResolveConnection(ctx context.Context, value Desktop) (ConnectionInfo, error) {
	if value.ID() == "" {
		return ConnectionInfo{}, fmt.Errorf("desktop: desktopId 为空")
	}
	if value.Forbidden {
		return ConnectionInfo{}, fmt.Errorf("desktop: 当前云电脑禁止连接")
	}
	origin := value.ConnectOrigin(c.masterOrigin)
	params, err := c.connectParams(value)
	if err != nil {
		return ConnectionInfo{}, err
	}
	if _, err := doJSON[connectEnvelope](ctx, c, origin, http.MethodPost, queryConnectDataPath, params, nil); err != nil {
		return ConnectionInfo{}, fmt.Errorf("desktop: queryConnectData: %w", err)
	}
	connected, err := doJSON[connectEnvelope](ctx, c, origin, http.MethodPost, connectPath, params, nil)
	if err != nil {
		return ConnectionInfo{}, fmt.Errorf("desktop: connect: %w", err)
	}
	if err := connected.DesktopInfo.Validate(); err != nil {
		return ConnectionInfo{}, err
	}
	return connected.DesktopInfo, nil
}

type connectEnvelope struct {
	GoingRetry  bool           `json:"goingRetry"`
	DesktopInfo ConnectionInfo `json:"desktopInfo"`
}

func (c *Client) connectParams(value Desktop) (url.Values, error) {
	identity := c.auth.Identity()
	device := c.auth.Device()
	if device.Code == "" {
		return nil, fmt.Errorf("desktop: deviceCode 为空")
	}
	appVersion := identity.AppVersion
	parts := strings.Split(appVersion, ".")
	if len(parts) > 3 {
		appVersion = strings.Join(parts[:3], ".")
	}
	vdCommand, err := json.Marshal(map[string]any{
		"command": 9,
		"data": map[string]any{
			"monitors": []map[string]int{{
				"width": 1920, "height": 1080, "scale": 100, "depth": 32, "x": 0, "y": 0,
			}},
		},
	})
	if err != nil {
		return nil, err
	}
	return url.Values{
		"objId":                 {value.ID()},
		"objType":               {strconv.Itoa(value.ObjectType)},
		"osType":                {"15"}, // ArkEnvTypes.OSType.WINDOWS
		"deviceId":              {identity.DeviceType},
		"deviceCode":            {device.Code},
		"deviceName":            {identity.DeviceName},
		"sysVersion":            {identity.SysVersion},
		"appVersion":            {appVersion},
		"hostName":              {identity.DeviceName},
		"vdCommand":             {string(vdCommand)},
		"ipAddress":             {""},
		"macAddress":            {""},
		"hardwareFeatureCode":   {device.Code},
		"loginDesktopType":      {"1"},
		"specifiedCertCategory": {"1"},
		"snCode":                {""},
	}, nil
}

func doJSON[T any](ctx context.Context, client *Client, origin, method, path string, form url.Values, body any) (T, error) {
	var zero T
	serverData, err := client.auth.GetServerData(ctx, origin)
	if err != nil {
		return zero, fmt.Errorf("desktop: 获取 %s serverNode: %w", origin, err)
	}
	headers, err := client.auth.ServerNodeHeaders(path, serverData.ServerNodeID)
	if err != nil {
		return zero, err
	}

	var payload io.Reader
	if form != nil {
		payload = strings.NewReader(form.Encode())
		headers.Set("Content-Type", "application/x-www-form-urlencoded")
	} else if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return zero, fmt.Errorf("desktop: 编码 %s 请求: %w", path, err)
		}
		payload = bytes.NewReader(raw)
		headers.Set("Content-Type", "application/json")
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(origin, "/")+path, payload)
	if err != nil {
		return zero, err
	}
	request.Header = headers
	response, err := client.auth.Do(request)
	if err != nil {
		return zero, fmt.Errorf("desktop: %s: %w", path, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return zero, fmt.Errorf("desktop: %s HTTP %d", path, response.StatusCode)
	}
	if encryptedType := response.Header.Get("CTG-RSPDATA-ETYPE"); encryptedType != "" {
		return zero, fmt.Errorf("desktop: %s 返回加密响应类型 %s，当前会话未协商请求加密", path, encryptedType)
	}
	var envelope struct {
		Code    int    `json:"code"`
		Message string `json:"msg"`
		Data    T      `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return zero, fmt.Errorf("desktop: 解析 %s: %w", path, err)
	}
	if envelope.Code != 0 {
		if envelope.Code == auth.CodeNodeSignatureMismatch {
			client.auth.InvalidateServerData(origin)
		}
		return zero, auth.APIError{Code: envelope.Code, Message: envelope.Message}
	}
	return envelope.Data, nil
}
