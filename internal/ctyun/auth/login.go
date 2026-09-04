package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type LoginChallenge struct {
	ID         string
	Code       string
	CaptchaKey string
	Captcha    []byte
}

func (c *Client) BeginLogin(ctx context.Context, account string) (LoginChallenge, error) {
	requestContext, headers, err := c.BaseHeaders()
	_ = requestContext
	if err != nil {
		return LoginChallenge{}, err
	}
	headers.Set("Content-Type", "application/json")
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiOrigin+"/api/auth/client/genChallengeData", bytes.NewReader([]byte("{}")))
	if err != nil {
		return LoginChallenge{}, err
	}
	request.Header = headers
	response, err := c.http.Do(request)
	if err != nil {
		return LoginChallenge{}, fmt.Errorf("auth: 获取登录 challenge: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return LoginChallenge{}, fmt.Errorf("auth: challenge HTTP %d", response.StatusCode)
	}
	var envelope apiEnvelope[struct {
		ChallengeID   string `json:"challengeId"`
		ChallengeCode string `json:"challengeCode"`
	}]
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return LoginChallenge{}, fmt.Errorf("auth: 解析 challenge: %w", err)
	}
	if envelope.Code != 0 {
		return LoginChallenge{}, APIError{Code: envelope.Code, Message: envelope.Message}
	}
	if envelope.Data.ChallengeID == "" || envelope.Data.ChallengeCode == "" {
		return LoginChallenge{}, fmt.Errorf("auth: challenge 响应缺少必要字段")
	}

	captcha, captchaKey, err := c.downloadLoginCaptcha(ctx, account)
	if err != nil {
		return LoginChallenge{}, err
	}
	return LoginChallenge{
		ID:         envelope.Data.ChallengeID,
		Code:       envelope.Data.ChallengeCode,
		CaptchaKey: captchaKey,
		Captcha:    captcha,
	}, nil
}

func (c *Client) Login(ctx context.Context, account, password, captchaCode string, challenge LoginChallenge) (Profile, error) {
	if challenge.ID == "" || challenge.Code == "" || captchaCode == "" {
		return Profile{}, fmt.Errorf("auth: 登录参数不完整")
	}
	_, headers, err := c.BaseHeaders()
	if err != nil {
		return Profile{}, err
	}
	form := url.Values{
		"deviceCode":    {c.device.Code},
		"deviceName":    {NativeDeviceName},
		"deviceType":    {NativeDeviceType},
		"deviceModel":   {NativeDeviceModel},
		"appVersion":    {NativeAppVersion},
		"sysVersion":    {NativeSysVersion},
		"clientVersion": {NativeVersion},
		"userAccount":   {account},
		"password":      {LoginPassword(password, challenge.Code)},
		"challengeId":   {challenge.ID},
		"captchaCode":   {captchaCode},
	}
	if challenge.CaptchaKey != "" {
		form.Set("captchaCodeKey", challenge.CaptchaKey)
	}
	headers.Set("Content-Type", "application/x-www-form-urlencoded")
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiOrigin+"/api/auth/client/login", bytes.NewBufferString(form.Encode()))
	if err != nil {
		return Profile{}, err
	}
	request.Header = headers
	response, err := c.http.Do(request)
	if err != nil {
		return Profile{}, fmt.Errorf("auth: 登录请求: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Profile{}, fmt.Errorf("auth: login HTTP %d", response.StatusCode)
	}
	var envelope apiEnvelope[loginData]
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return Profile{}, fmt.Errorf("auth: 解析登录响应: %w", err)
	}
	if envelope.Code != 0 {
		return Profile{}, APIError{Code: envelope.Code, Message: envelope.Message}
	}
	profile, err := profileFromLogin(envelope.Data, c.now())
	if err != nil {
		return Profile{}, err
	}
	c.UseProfile(profile)
	return profile, nil
}

func (c *Client) downloadLoginCaptcha(ctx context.Context, account string) ([]byte, string, error) {
	_, headers, err := c.BaseHeaders()
	if err != nil {
		return nil, "", err
	}
	params := url.Values{
		"width":    {"100"},
		"height":   {"40"},
		"userInfo": {account},
		"_t":       {strconv.FormatInt(c.now().UnixMilli(), 10)},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiOrigin+"/api/auth/client/captcha?"+params.Encode(), nil)
	if err != nil {
		return nil, "", err
	}
	request.Header = headers
	response, err := c.http.Do(request)
	if err != nil {
		return nil, "", fmt.Errorf("auth: 获取验证码: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("auth: captcha HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return nil, "", fmt.Errorf("auth: 读取验证码: %w", err)
	}
	if len(body) < 16 {
		return nil, "", fmt.Errorf("auth: 验证码响应过短")
	}
	return body, response.Header.Get("CTG-CAPTCHA-KEY"), nil
}

type loginData struct {
	UserID               int64  `json:"userId"`
	UserEID              string `json:"userEid"`
	TenantID             int64  `json:"tenantId"`
	SecretKey            string `json:"secretKey"`
	CommonLoginReqHeader string `json:"commonLoginReqHeader"`
	UserName             string `json:"userName"`
	MobilePhone          string `json:"mobilephone"`
	BondedDevice         bool   `json:"bondedDevice"`
	Timestamp            int64  `json:"timestamp"`
	OffsetTime           int64  `json:"offsetTime"`
}

func profileFromLogin(data loginData, now time.Time) (Profile, error) {
	if data.UserID == 0 || data.UserEID == "" || data.TenantID == 0 || data.SecretKey == "" || data.CommonLoginReqHeader == "" {
		return Profile{}, fmt.Errorf("auth: 登录响应缺少原生鉴权字段")
	}
	offsetMillis := data.OffsetTime
	if offsetMillis == 0 && data.Timestamp != 0 {
		offsetMillis = now.UnixMilli() - data.Timestamp
	}
	return Profile{
		UserID:               data.UserID,
		UserEID:              data.UserEID,
		TenantID:             data.TenantID,
		SecretKey:            data.SecretKey,
		CommonLoginReqHeader: data.CommonLoginReqHeader,
		UserName:             data.UserName,
		MobilePhone:          data.MobilePhone,
		BondedDevice:         data.BondedDevice,
		Offset:               time.Duration(offsetMillis) * time.Millisecond,
	}, nil
}
