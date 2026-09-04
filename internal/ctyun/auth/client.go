package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const DefaultAPIOrigin = "https://desk.ctyun.cn:8810"

type ClientOptions struct {
	APIOrigin  string
	HTTPClient *http.Client
	Now        func() time.Time
	Random     io.Reader
	Identity   ClientIdentity
}

type Client struct {
	apiOrigin string
	http      *http.Client
	device    DeviceIdentity
	identity  ClientIdentity
	profile   *Profile
	now       func() time.Time
	random    io.Reader
	tick      atomic.Uint64
	serverMu  sync.RWMutex
	servers   map[string]ServerData
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
		identity:  options.Identity.withDefaults(),
		now:       now,
		random:    randomSource,
		servers:   make(map[string]ServerData),
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

func (c *Client) Device() DeviceIdentity {
	return c.device
}

func (c *Client) Identity() ClientIdentity {
	return c.identity
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
	headers.Set("CTG-DEVICETYPE", c.identity.DeviceType)
	headers.Set("CTG-REQUESTID", ctx.RequestID)
	headers.Set("CTG-TIMESTAMP", ctx.Timestamp)
	headers.Set("CTG-VERSION", c.identity.Version)
	headers.Set("CTG-APPMODEL", c.identity.AppModel)
	headers.Set("CTG-APPCHANNEL", c.identity.AppChannel)
	headers.Set("CTG-DEVICE-MODEL", c.identity.DeviceModel)
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
	headers.Set("CTG-SIGNATURESTR", PublicSignatureWithIdentity(c.identity, requestContext, profile))
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
	headers.Set("CTG-SIGNATURE", ServerNodeSignatureWithIdentity(c.identity, requestContext, profile, serverNode))
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

// GetServerData 获取指定业管 Host 的 serverNodeId。官方客户端把 serverNode 绑定到 Host，
// 因此区域 deskmgr 不能复用主站节点。该接口本身不要求请求体加密。
func (c *Client) GetServerData(ctx context.Context, origin string) (ServerData, error) {
	const path = "/api/cdserv/client/getServData"
	origin = strings.TrimRight(origin, "/")
	if origin == "" {
		origin = c.apiOrigin
	}
	c.serverMu.RLock()
	cached, ok := c.servers[origin]
	c.serverMu.RUnlock()
	if ok && cached.ServerNodeID != "" {
		return cached, nil
	}

	var headers http.Header
	var err error
	if c.profile != nil {
		headers, err = c.PublicHeaders(path)
	} else {
		_, headers, err = c.BaseHeaders()
	}
	if err != nil {
		return ServerData{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+path, nil)
	if err != nil {
		return ServerData{}, err
	}
	request.Header = headers
	response, err := c.http.Do(request)
	if err != nil {
		return ServerData{}, fmt.Errorf("auth: getServData: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ServerData{}, fmt.Errorf("auth: getServData HTTP %d", response.StatusCode)
	}
	var envelope apiEnvelope[ServerData]
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return ServerData{}, fmt.Errorf("auth: 解析 getServData: %w", err)
	}
	if envelope.Code != 0 {
		return ServerData{}, APIError{Code: envelope.Code, Message: envelope.Message}
	}
	if envelope.Data.ServerNodeID == "" {
		return ServerData{}, fmt.Errorf("auth: getServData 未返回 serverNodeId")
	}
	c.serverMu.Lock()
	c.servers[origin] = envelope.Data
	c.serverMu.Unlock()
	return envelope.Data, nil
}

const (
	CodeNodeSignatureMismatch = 30021
	CodeDeviceUnbound         = 30060
	CodeNoPermissions         = 40010
)

func ErrorCode(err error) (int, bool) {
	var apiError APIError
	if !errors.As(err, &apiError) {
		return 0, false
	}
	return apiError.Code, true
}

func RequiresAuthentication(err error) bool {
	code, ok := ErrorCode(err)
	return ok && code == CodeNoPermissions
}

func RequiresDeviceBinding(err error) bool {
	code, ok := ErrorCode(err)
	return ok && code == CodeDeviceUnbound
}

// InvalidateServerData 只清理指定 Host 的节点缓存。节点签名失败后下一次请求会重新发现，
// 避免把一个区域的 serverNode 错误复用到另一个区域或长期持有过期节点。
func (c *Client) InvalidateServerData(origin string) {
	origin = strings.TrimRight(origin, "/")
	if origin == "" {
		origin = c.apiOrigin
	}
	c.serverMu.Lock()
	delete(c.servers, origin)
	c.serverMu.Unlock()
}
