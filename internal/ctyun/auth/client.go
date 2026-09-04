package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const DefaultAPIOrigin = "https://desk.ctyun.cn:8810"

type ClientOptions struct {
	APIOrigin  string
	HTTPClient *http.Client
	Now        func() time.Time
	Random     io.Reader
}

type Client struct {
	apiOrigin string
	http      *http.Client
	device    DeviceIdentity
	profile   *Profile
	now       func() time.Time
	random    io.Reader
	tick      atomic.Uint64
}

func NewClient(device DeviceIdentity, options ClientOptions) *Client {
	origin := strings.TrimRight(options.APIOrigin, "/")
	if origin == "" {
		origin = DefaultAPIOrigin
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	randomSource := options.Random
	if randomSource == nil {
		randomSource = rand.Reader
	}
	return &Client{
		apiOrigin: origin,
		http:      httpClient,
		device:    device,
		now:       now,
		random:    randomSource,
	}
}

func (c *Client) UseProfile(profile Profile) {
	c.profile = &profile
}

func (c *Client) Profile() (Profile, bool) {
	if c.profile == nil {
		return Profile{}, false
	}
	return *c.profile, true
}

func (c *Client) Do(request *http.Request) (*http.Response, error) {
	return c.http.Do(request)
}

func (c *Client) APIOrigin() string {
	return c.apiOrigin
}

func (c *Client) BaseHeaders() (RequestContext, http.Header, error) {
	now := c.now()
	offset := time.Duration(0)
	if c.profile != nil {
		offset = c.profile.Offset
	}
	timestamp := now.Add(-offset).UnixMilli()
	requestID := now.UnixMilli() + int64(c.tick.Add(1))
	randomValue, err := randomAlphaNumeric(c.random, 8)
	if err != nil {
		return RequestContext{}, nil, err
	}
	traceID, err := randomUUID(c.random)
	if err != nil {
		return RequestContext{}, nil, err
	}
	ctx := RequestContext{
		RequestID: strconv.FormatInt(requestID, 10),
		Timestamp: strconv.FormatInt(timestamp, 10),
	}
	headers := make(http.Header)
	headers.Set("CTG-DEVICECODE", c.device.Code)
	headers.Set("CTG-DEVICETYPE", NativeDeviceType)
	headers.Set("CTG-REQUESTID", ctx.RequestID)
	headers.Set("CTG-TIMESTAMP", ctx.Timestamp)
	headers.Set("CTG-VERSION", NativeVersion)
	headers.Set("CTG-APPMODEL", NativeAppModel)
	headers.Set("CTG-APPCHANNEL", NativeAppChannel)
	headers.Set("CTG-DEVICE-MODEL", NativeDeviceModel)
	headers.Set("x-random", randomValue)
	headers.Set("x-product-id", "7")
	headers.Set("x-client-trace-id", traceID)
	return ctx, headers, nil
}

func (c *Client) PublicHeaders(path string) (http.Header, error) {
	profile, ok := c.Profile()
	if !ok {
		return nil, fmt.Errorf("auth: 尚未登录")
	}
	requestContext, headers, err := c.BaseHeaders()
	if err != nil {
		return nil, err
	}
	requestContext.Path = path
	headers.Set("CTG-TENANTID", strconv.FormatInt(profile.TenantID, 10))
	headers.Set("CTG-USERID", strconv.FormatInt(profile.UserID, 10))
	headers.Set("CTG-SIGNATURESTR", PublicSignature(requestContext, profile))
	headers.Set("CTG-COMMON-DATA", profile.CommonLoginReqHeader)
	return headers, nil
}

func (c *Client) ServerNodeHeaders(path, serverNode string) (http.Header, error) {
	profile, ok := c.Profile()
	if !ok {
		return nil, fmt.Errorf("auth: 尚未登录")
	}
	requestContext, headers, err := c.BaseHeaders()
	if err != nil {
		return nil, err
	}
	requestContext.Path = path
	headers.Set("CTG-SERVERNODE", serverNode)
	headers.Set("CTG-TENANTID", strconv.FormatInt(profile.TenantID, 10))
	headers.Set("CTG-USEREID", profile.UserEID)
	headers.Set("CTG-SIGNATURE", ServerNodeSignature(requestContext, profile, serverNode))
	headers.Set("CTG-COMMON-DATA", profile.CommonLoginReqHeader)
	return headers, nil
}

func (c *Client) GetTicket(ctx context.Context, service string) (string, error) {
	const path = "/api/auth/client/getTicket"
	headers, err := c.PublicHeaders(path)
	if err != nil {
		return "", err
	}
	endpoint := c.apiOrigin + path + "?" + url.Values{"service": {service}}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	request.Header = headers
	response, err := c.http.Do(request)
	if err != nil {
		return "", fmt.Errorf("auth: getTicket: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("auth: getTicket HTTP %d", response.StatusCode)
	}
	var envelope apiEnvelope[struct {
		Ticket string `json:"ticket"`
	}]
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return "", fmt.Errorf("auth: 解析 getTicket: %w", err)
	}
	if envelope.Code != 0 {
		return "", APIError{Code: envelope.Code, Message: envelope.Message}
	}
	if envelope.Data.Ticket == "" {
		return "", fmt.Errorf("auth: getTicket 未返回 ticket")
	}
	return envelope.Data.Ticket, nil
}

func randomAlphaNumeric(source io.Reader, length int) (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	buf := make([]byte, length)
	raw := make([]byte, length)
	if _, err := io.ReadFull(source, raw); err != nil {
		return "", fmt.Errorf("auth: 生成随机数: %w", err)
	}
	for i, value := range raw {
		buf[i] = alphabet[int(value)%len(alphabet)]
	}
	return string(buf), nil
}

func randomUUID(source io.Reader) (string, error) {
	var raw [16]byte
	if _, err := io.ReadFull(source, raw[:]); err != nil {
		return "", fmt.Errorf("auth: 生成 trace id: %w", err)
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := make([]byte, 32)
	hex.Encode(encoded, raw[:])
	return string(encoded[0:8]) + "-" + string(encoded[8:12]) + "-" + string(encoded[12:16]) + "-" + string(encoded[16:20]) + "-" + string(encoded[20:32]), nil
}

type apiEnvelope[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"msg"`
	Data    T      `json:"data"`
}

type APIError struct {
	Code    int
	Message string
}

func (e APIError) Error() string {
	return fmt.Sprintf("auth: code=%d: %s", e.Code, e.Message)
}
