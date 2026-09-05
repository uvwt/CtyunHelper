package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/uvwt/CtyunHelper/internal/ctyun/auth"
)

// ListForKeepalive 复现旧 CtYun 保活链的云电脑列表请求。
// 它与现代 Windows 客户端的 List 分开，避免两代 Profile 和签名体系互相污染。
func (c *Client) ListForKeepalive(ctx context.Context) ([]Desktop, error) {
	body := struct {
		Count        int      `json:"getCnt"`
		DesktopTypes []string `json:"desktopTypes"`
		SortType     string   `json:"sortType"`
	}{
		Count:        20,
		DesktopTypes: []string{"1", "2001", "2002", "2003"},
		SortType:     "createTimeV1",
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("desktop: 编码 Clink 云电脑列表: %w", err)
	}
	headers, err := c.auth.LegacyClinkHeaders()
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.masterOrigin+pageDesktopPath, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	request.Header = headers
	request.Header.Set("Content-Type", "application/json")
	response, err := c.auth.Do(request)
	if err != nil {
		return nil, fmt.Errorf("desktop: Clink pageDesktop: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("desktop: Clink pageDesktop HTTP %d", response.StatusCode)
	}
	var envelope struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			DesktopList []Desktop `json:"desktopList"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("desktop: 解析 Clink pageDesktop: %w", err)
	}
	if envelope.Code != 0 {
		return nil, auth.APIError{Code: envelope.Code, Message: envelope.Msg}
	}
	return envelope.Data.DesktopList, nil
}

// ResolveClinkConnection 只获取旧保活链对应的 Clink 路由。
// 现代 queryConnectData -> connect 仍由 ResolveConnection 独立负责。
func (c *Client) ResolveClinkConnection(ctx context.Context, value Desktop) (ConnectionInfo, error) {
	if value.ID() == "" {
		return ConnectionInfo{}, fmt.Errorf("desktop: desktopId 为空")
	}
	if value.Forbidden {
		return ConnectionInfo{}, fmt.Errorf("desktop: 当前云电脑禁止连接")
	}
	deviceCode := c.auth.Device().Code
	if deviceCode == "" {
		return ConnectionInfo{}, fmt.Errorf("desktop: deviceCode 为空")
	}
	identity := auth.LegacyClinkIdentity()
	form := url.Values{
		"objId":         {value.ID()},
		"objType":       {"0"},
		"osType":        {"15"},
		"deviceId":      {identity.DeviceType},
		"vdCommand":     {""},
		"ipAddress":     {""},
		"macAddress":    {""},
		"deviceCode":    {deviceCode},
		"deviceName":    {identity.DeviceName},
		"deviceType":    {identity.DeviceType},
		"deviceModel":   {identity.DeviceModel},
		"appVersion":    {identity.AppVersion},
		"sysVersion":    {identity.SysVersion},
		"clientVersion": {identity.Version},
	}
	headers, err := c.auth.LegacyClinkHeaders()
	if err != nil {
		return ConnectionInfo{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.masterOrigin+connectPath, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return ConnectionInfo{}, err
	}
	request.Header = headers
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := c.auth.Do(request)
	if err != nil {
		return ConnectionInfo{}, fmt.Errorf("desktop: Clink connect: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ConnectionInfo{}, fmt.Errorf("desktop: Clink connect HTTP %d", response.StatusCode)
	}
	var envelope struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			DesktopInfo ConnectionInfo `json:"desktopInfo"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return ConnectionInfo{}, fmt.Errorf("desktop: 解析 Clink connect: %w", err)
	}
	if envelope.Code != 0 {
		return ConnectionInfo{}, auth.APIError{Code: envelope.Code, Message: envelope.Msg}
	}
	if err := envelope.Data.DesktopInfo.Validate(); err != nil {
		return ConnectionInfo{}, err
	}
	return envelope.Data.DesktopInfo, nil
}
