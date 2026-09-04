package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type DeviceBindingChallenge struct {
	Captcha    []byte
	CaptchaKey string
	Mobile     string
}

// BeginDeviceBinding 下载官方设备绑定验证码。登录返回 bondedDevice=false 后才需要调用。
func (c *Client) BeginDeviceBinding(ctx context.Context) (DeviceBindingChallenge, error) {
	profile, ok := c.Profile()
	if !ok {
		return DeviceBindingChallenge{}, fmt.Errorf("auth: 尚未登录")
	}
	if profile.BondedDevice {
		return DeviceBindingChallenge{}, fmt.Errorf("auth: 当前设备已经绑定")
	}
	if profile.MobilePhone == "" {
		return DeviceBindingChallenge{}, fmt.Errorf("auth: 登录响应未提供绑定手机号")
	}
	const path = "/api/auth/client/validateCode/captcha"
	headers, err := c.serverHeaders(ctx, path)
	if err != nil {
		return DeviceBindingChallenge{}, err
	}
	params := url.Values{
		"width":  {"100"},
		"height": {"50"},
		"_t":     {strconv.FormatInt(c.now().UnixMilli(), 10)},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiOrigin+path+"?"+params.Encode(), nil)
	if err != nil {
		return DeviceBindingChallenge{}, err
	}
	request.Header = headers
	response, err := c.http.Do(request)
	if err != nil {
		return DeviceBindingChallenge{}, fmt.Errorf("auth: 获取设备绑定验证码: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return DeviceBindingChallenge{}, fmt.Errorf("auth: 设备验证码 HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return DeviceBindingChallenge{}, fmt.Errorf("auth: 读取设备验证码: %w", err)
	}
	if len(body) < 16 {
		return DeviceBindingChallenge{}, fmt.Errorf("auth: 设备验证码响应过短")
	}
	key := response.Header.Get("CTG-CAPTCHA-KEY")
	if key == "" {
		return DeviceBindingChallenge{}, fmt.Errorf("auth: 设备验证码缺少 CTG-CAPTCHA-KEY")
	}
	return DeviceBindingChallenge{Captcha: body, CaptchaKey: key, Mobile: profile.MobilePhone}, nil
}

func (c *Client) SendDeviceSMS(ctx context.Context, captchaCode, captchaKey string) (string, error) {
	profile, ok := c.Profile()
	if !ok {
		return "", fmt.Errorf("auth: 尚未登录")
	}
	if profile.MobilePhone == "" || strings.TrimSpace(captchaCode) == "" || captchaKey == "" {
		return "", fmt.Errorf("auth: 设备短信参数不完整")
	}
	const path = "/api/cdserv/client/device/getSmsCode"
	headers, err := c.serverHeaders(ctx, path)
	if err != nil {
		return "", err
	}
	params := url.Values{
		"mobilePhone":    {profile.MobilePhone},
		"captchaCode":    {strings.TrimSpace(captchaCode)},
		"captchaCodeKey": {captchaKey},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiOrigin+path+"?"+params.Encode(), nil)
	if err != nil {
		return "", err
	}
	request.Header = headers
	response, err := c.http.Do(request)
	if err != nil {
		return "", fmt.Errorf("auth: 发送设备绑定短信: %w", err)
	}
	defer response.Body.Close()
	if err := rejectEncryptedResponse(response, path); err != nil {
		return "", err
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("auth: 设备短信 HTTP %d", response.StatusCode)
	}
	var envelope apiEnvelope[json.RawMessage]
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return "", fmt.Errorf("auth: 解析设备短信响应: %w", err)
	}
	if envelope.Code != 0 {
		return "", APIError{Code: envelope.Code, Message: envelope.Message}
	}
	smsKey := response.Header.Get("CTG-SMS-KEY")
	if smsKey == "" {
		return "", fmt.Errorf("auth: 设备短信响应缺少 CTG-SMS-KEY")
	}
	return smsKey, nil
}

func (c *Client) BindDevice(ctx context.Context, smsCode, smsKey string) error {
	profile, ok := c.Profile()
	if !ok {
		return fmt.Errorf("auth: 尚未登录")
	}
	if strings.TrimSpace(smsCode) == "" || smsKey == "" {
		return fmt.Errorf("auth: 短信验证码不能为空")
	}
	const path = "/api/cdserv/client/device/binding"
	headers, err := c.serverHeaders(ctx, path)
	if err != nil {
		return err
	}
	headers.Set("Content-Type", "application/x-www-form-urlencoded")
	hostName, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostName) == "" {
		hostName = c.identity.DeviceName
	}
	form := url.Values{
		"verificationCode": {strings.TrimSpace(smsCode)},
		"smsCodeKey":       {smsKey},
		"deviceName":       {c.identity.DeviceName},
		"deviceCode":       {c.device.Code},
		"deviceModel":      {c.identity.DeviceModel},
		"sysVersion":       {c.identity.SysVersion},
		// 官方 ArkSecurityService.bindDevice 使用完整 __APP_VERSION，
		// 与 connect 表单只取前三段版本号的行为不同。
		"appVersion": {c.identity.AppVersion},
		"hostName":   {hostName},
		"deviceInfo": {"windows"},
		// 官方值来自 ArkExtends.system.deviceSN()；无法可靠取得时官方同样回退为空串。
		"snCode": {""},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiOrigin+path, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return err
	}
	request.Header = headers
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("auth: 绑定设备: %w", err)
	}
	defer response.Body.Close()
	if err := rejectEncryptedResponse(response, path); err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("auth: 绑定设备 HTTP %d", response.StatusCode)
	}
	var envelope apiEnvelope[json.RawMessage]
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("auth: 解析设备绑定响应: %w", err)
	}
	if envelope.Code != 0 {
		return APIError{Code: envelope.Code, Message: envelope.Message}
	}
	profile.BondedDevice = true
	c.UseProfile(profile)
	return nil
}

func (c *Client) serverHeaders(ctx context.Context, path string) (http.Header, error) {
	serverData, err := c.GetServerData(ctx, c.apiOrigin)
	if err != nil {
		return nil, err
	}
	return c.ServerNodeHeaders(path, serverData.ServerNodeID)
}

func rejectEncryptedResponse(response *http.Response, path string) error {
	if encryptedType := response.Header.Get("CTG-RSPDATA-ETYPE"); encryptedType != "" {
		return fmt.Errorf("auth: %s 返回加密响应类型 %s，当前会话未协商请求加密", path, encryptedType)
	}
	return nil
}
