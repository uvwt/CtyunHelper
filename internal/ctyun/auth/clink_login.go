package auth

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // 天翼旧 Clink 鉴权协议固定使用 MD5。
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// LoginLegacyClink 使用旧 CtYun 保活链的独立终端身份建立 Clink 专用 Profile。
// 该 Profile 不能与现代 Windows 客户端 Profile 混用。
func (c *Client) LoginLegacyClink(ctx context.Context, account, password string) (Profile, error) {
	if account == "" || password == "" {
		return Profile{}, fmt.Errorf("auth: Clink 登录账号和密码不能为空")
	}
	challengeID, challengeCode, err := c.beginLegacyClinkLogin(ctx)
	if err != nil {
		return Profile{}, err
	}

	identity := LegacyClinkIdentity()
	form := url.Values{
		"deviceCode":     {c.device.Code},
		"deviceName":     {identity.DeviceName},
		"deviceType":     {identity.DeviceType},
		"deviceModel":    {identity.DeviceModel},
		"appVersion":     {identity.AppVersion},
		"sysVersion":     {identity.SysVersion},
		"clientVersion":  {identity.Version},
		"userAccount":    {account},
		"password":       {SHA256Hex(password + challengeCode)},
		"sha256Password": {LoginPassword(password, challengeCode)},
		"challengeId":    {challengeID},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiOrigin+"/api/auth/client/login", bytes.NewBufferString(form.Encode()))
	if err != nil {
		return Profile{}, err
	}
	request.Header = c.legacyClinkBaseHeaders()
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := c.http.Do(request)
	if err != nil {
		return Profile{}, fmt.Errorf("auth: Clink 登录请求: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Profile{}, fmt.Errorf("auth: Clink login HTTP %d", response.StatusCode)
	}
	var envelope apiEnvelope[loginData]
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return Profile{}, fmt.Errorf("auth: 解析 Clink 登录响应: %w", err)
	}
	if envelope.Code != 0 {
		return Profile{}, APIError{Code: envelope.Code, Message: envelope.Message}
	}
	return profileFromLogin(envelope.Data, c.now())
}

func (c *Client) beginLegacyClinkLogin(ctx context.Context) (string, string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiOrigin+"/api/auth/client/genChallengeData", bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", "", err
	}
	request.Header = c.legacyClinkBaseHeaders()
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return "", "", fmt.Errorf("auth: 获取 Clink 登录 challenge: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("auth: Clink challenge HTTP %d", response.StatusCode)
	}
	var envelope apiEnvelope[struct {
		ChallengeID   string `json:"challengeId"`
		ChallengeCode string `json:"challengeCode"`
	}]
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return "", "", fmt.Errorf("auth: 解析 Clink challenge: %w", err)
	}
	if envelope.Code != 0 {
		return "", "", APIError{Code: envelope.Code, Message: envelope.Message}
	}
	if envelope.Data.ChallengeID == "" || envelope.Data.ChallengeCode == "" {
		return "", "", fmt.Errorf("auth: Clink challenge 响应缺少必要字段")
	}
	return envelope.Data.ChallengeID, envelope.Data.ChallengeCode, nil
}

// LegacyClinkHeaders 生成旧 Clink 列表/connect 接口使用的独立鉴权头。
func (c *Client) LegacyClinkHeaders() (http.Header, error) {
	profile, ok := c.Profile()
	if !ok {
		return nil, fmt.Errorf("auth: 尚未建立 Clink Profile")
	}
	identity := LegacyClinkIdentity()
	timestamp := strconv.FormatInt(c.now().UnixMilli(), 10)
	source := identity.DeviceType + timestamp + strconv.FormatInt(profile.TenantID, 10) + timestamp +
		strconv.FormatInt(profile.UserID, 10) + identity.Version + profile.SecretKey
	digest := md5.Sum([]byte(source))

	headers := c.legacyClinkBaseHeaders()
	headers.Set("ctg-userid", strconv.FormatInt(profile.UserID, 10))
	headers.Set("ctg-tenantid", strconv.FormatInt(profile.TenantID, 10))
	headers.Set("ctg-timestamp", timestamp)
	headers.Set("ctg-requestid", timestamp)
	headers.Set("ctg-signaturestr", hex.EncodeToString(digest[:]))
	return headers, nil
}

func (c *Client) legacyClinkBaseHeaders() http.Header {
	identity := LegacyClinkIdentity()
	headers := make(http.Header)
	headers.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/137.0.0.0 Safari/537.36")
	headers.Set("Referer", "https://pc.ctyun.cn/")
	headers.Set("ctg-devicetype", identity.DeviceType)
	headers.Set("ctg-version", identity.Version)
	headers.Set("ctg-devicecode", c.device.Code)
	return headers
}
