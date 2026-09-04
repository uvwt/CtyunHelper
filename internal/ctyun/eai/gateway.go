package eai

import (
	"context"
	"crypto/rsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

func (c *Client) Authorize(ctx context.Context) error {
	// 旧脚本每次任务都会创建新进程；常驻 Windows 进程不能无限复用旧的
	// EAI sessionKey/tenant。每次任务重新授权，既规避 Session 过期，也避免
	// 更换天翼账号后继续携带上一个账号的租户信息。
	c.sessionKey = ""
	c.tenant = nil
	gateway, err := c.FetchGateway(ctx)
	if err != nil {
		return err
	}
	if gateway.SSO.PublicKey == "" || gateway.SSO.PublicKeyID == "" {
		return fmt.Errorf("eai: 网关未返回 SSO 公钥")
	}
	publicKey, err := parseRSAPublicKey(gateway.SSO.PublicKey)
	if err != nil {
		return err
	}
	clientKey, err := randomAlphaNumeric(c.random, 16)
	if err != nil {
		return err
	}
	encryptedKey, err := rsa.EncryptPKCS1v15(c.random, publicKey, []byte(clientKey))
	if err != nil {
		return fmt.Errorf("eai: 加密 SSO clientKey: %w", err)
	}
	ticket, err := c.tickets.GetTicket(ctx, ServiceURL)
	if err != nil {
		return fmt.Errorf("eai: 获取 IAM Ticket: %w", err)
	}

	headers, err := c.baseHeaders("")
	if err != nil {
		return err
	}
	headers.Set("Content-Type", "application/x-www-form-urlencoded")
	form := url.Values{
		"loginType":   {"iamTicket"},
		"clientId":    {"eaiapp"},
		"iamTicket":   {ticket},
		"redirectUri": {ServiceURL},
		"clientKey":   {hex.EncodeToString(encryptedKey)},
		"clientKeyId": {gateway.SSO.PublicKeyID},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.eaiHost+"/sso/login/v2/iam/ticketAuthorize", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	request.Header = headers
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("eai: Ticket 授权请求: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("eai: Ticket 授权 HTTP %d", response.StatusCode)
	}
	var envelope rawEnvelope
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("eai: 解析 Ticket 授权响应: %w", err)
	}
	if envelope.ResultCode != "0" {
		return resultError("Ticket 授权", envelope)
	}
	var data struct {
		SessionKey string `json:"sessionKey"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil || data.SessionKey == "" {
		return fmt.Errorf("eai: Ticket 授权未返回 sessionKey")
	}
	plainSessionKey, err := decryptAESECBBase64(data.SessionKey, []byte(clientKey))
	if err != nil {
		return fmt.Errorf("eai: 解密 sessionKey: %w", err)
	}
	if len(plainSessionKey) == 0 {
		return fmt.Errorf("eai: sessionKey 为空")
	}
	c.sessionKey = string(plainSessionKey)
	return nil
}

func (c *Client) FetchGateway(ctx context.Context) (Gateway, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.gatewayURL, nil)
	if err != nil {
		return Gateway{}, err
	}
	request.Header.Set("User-Agent", c.userAgent)
	response, err := c.http.Do(request)
	if err != nil {
		return Gateway{}, fmt.Errorf("eai: 获取网关: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Gateway{}, fmt.Errorf("eai: 获取网关 HTTP %d", response.StatusCode)
	}
	var envelope rawEnvelope
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return Gateway{}, fmt.Errorf("eai: 解析网关响应: %w", err)
	}
	if envelope.ResultCode != "0" {
		return Gateway{}, resultError("获取网关", envelope)
	}
	var encrypted string
	if err := json.Unmarshal(envelope.Data, &encrypted); err != nil || encrypted == "" {
		return Gateway{}, fmt.Errorf("eai: 网关未返回加密配置")
	}
	plain, err := decryptAESECBBase64(encrypted, gatewayAESKey)
	if err != nil {
		return Gateway{}, fmt.Errorf("eai: 解密网关配置: %w", err)
	}
	var gateway Gateway
	if err := json.Unmarshal(plain, &gateway); err != nil {
		return Gateway{}, fmt.Errorf("eai: 解析网关配置: %w", err)
	}
	c.eaiHost = endpointFromGateway(gateway.EAI, c.eaiHost)
	return gateway, nil
}
