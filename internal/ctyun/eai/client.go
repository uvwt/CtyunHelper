package eai

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultGatewayURL = "https://gwyilian.ctyun.cn/server/eaiSysInfo"
	DefaultHost       = "https://eaichat.ctyun.cn:443"
	ServiceURL        = "https://eaichat.ctyun.cn:443/chat/#/aichat"
	Version           = "202060305"
	ProductID         = "5"
	defaultUserAgent  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/137.0.0.0 Safari/537.36"
)

type TicketProvider interface {
	GetTicket(context.Context, string) (string, error)
}

type ClientOptions struct {
	GatewayURL  string
	DefaultHost string
	HTTPClient  *http.Client
	Now         func() time.Time
	Random      io.Reader
	UserAgent   string
}

type Client struct {
	tickets TicketProvider
	http    *http.Client

	gatewayURL string
	eaiHost    string
	userAgent  string
	now        func() time.Time
	random     io.Reader
	xuid       string

	sessionKey string
	tenant     *Tenant
}

type Gateway struct {
	EAI Endpoint `json:"eai"`
	SSO SSO      `json:"sso"`
}

type Endpoint struct {
	PrivateHost string `json:"privateHost"`
	IP          string `json:"ip"`
	SecurePort  string `json:"secuPort"`
	PlainPort   string `json:"blankPort"`
}

type SSO struct {
	PublicKey   string `json:"ssopk"`
	PublicKeyID string `json:"ssopkid"`
}

type Tenant struct {
	TenantID    int64  `json:"tenantId"`
	TenantIDStr string `json:"tenantIdStr"`
}

func (t Tenant) HeaderID() string {
	if t.TenantIDStr != "" {
		return t.TenantIDStr
	}
	if t.TenantID == 0 {
		return ""
	}
	return strconv.FormatInt(t.TenantID, 10)
}

type Model struct {
	KeyModel string `json:"keyModel"`
	Status   string `json:"status"`
	Type     string `json:"type"`
}

type ChatResult struct {
	Model          string
	TenantID       int64
	ConversationID string
	EventCount     int
	Finished       bool
}

type rawEnvelope struct {
	ResultCode codeValue       `json:"resultCode"`
	ResultMsg  string          `json:"resultMsg"`
	Data       json.RawMessage `json:"data"`
}

type codeValue string

func (c *codeValue) UnmarshalJSON(raw []byte) error {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		*c = ""
		return nil
	}
	if raw[0] == '"' {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		*c = codeValue(value)
		return nil
	}
	*c = codeValue(string(raw))
	return nil
}

func NewClient(tickets TicketProvider, options ClientOptions) (*Client, error) {
	if tickets == nil {
		return nil, fmt.Errorf("eai: ticket provider 不能为空")
	}
	gatewayURL := strings.TrimSpace(options.GatewayURL)
	if gatewayURL == "" {
		gatewayURL = DefaultGatewayURL
	}
	host := strings.TrimRight(strings.TrimSpace(options.DefaultHost), "/")
	if host == "" {
		host = DefaultHost
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 150 * time.Second}
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	randomSource := options.Random
	if randomSource == nil {
		randomSource = rand.Reader
	}
	userAgent := strings.TrimSpace(options.UserAgent)
	if userAgent == "" {
		userAgent = defaultUserAgent
	}
	xuid, err := randomUUID(randomSource)
	if err != nil {
		return nil, err
	}
	return &Client{
		tickets:    tickets,
		http:       httpClient,
		gatewayURL: gatewayURL,
		eaiHost:    host,
		userAgent:  userAgent,
		now:        now,
		random:     randomSource,
		xuid:       "pubweb_" + xuid,
	}, nil
}

func (c *Client) requestJSON(ctx context.Context, method, path string, query url.Values, body []byte) (rawEnvelope, error) {
	tenantID := ""
	if c.tenant != nil {
		tenantID = c.tenant.HeaderID()
	}
	headers, err := c.signedHeaders(query, body, tenantID)
	if err != nil {
		return rawEnvelope{}, err
	}
	endpoint := c.eaiHost + c.rewritePath(path)
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
		headers.Set("Content-Type", "application/json")
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return rawEnvelope{}, err
	}
	request.Header = headers
	response, err := c.http.Do(request)
	if err != nil {
		return rawEnvelope{}, fmt.Errorf("eai: %s: %w", path, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return rawEnvelope{}, fmt.Errorf("eai: %s HTTP %d", path, response.StatusCode)
	}
	var envelope rawEnvelope
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return rawEnvelope{}, fmt.Errorf("eai: 解析 %s: %w", path, err)
	}
	return envelope, nil
}

func (c *Client) baseHeaders(tenantID string) (http.Header, error) {
	traceID, err := randomUUID(c.random)
	if err != nil {
		return nil, err
	}
	headers := make(http.Header)
	headers.Set("User-Agent", c.userAgent)
	headers.Set("x-client-trace-id", traceID)
	headers.Set("x-eai-xuid", c.xuid)
	headers.Set("x-eai-env", "pubWeb")
	headers.Set("x-eai-version", Version)
	headers.Set("x-user-agent", c.userAgent)
	headers.Set("x-eai-source", "web-eai")
	headers.Set("YL-Main-Version", Version)
	headers.Set("YL-Product-Id", ProductID)
	if tenantID != "" {
		headers.Set("x-eai-tenant-id", tenantID)
	}
	return headers, nil
}

func (c *Client) signedHeaders(query url.Values, body []byte, tenantID string) (http.Header, error) {
	headers, err := c.baseHeaders(tenantID)
	if err != nil {
		return nil, err
	}
	if c.sessionKey == "" {
		return headers, nil
	}
	parts := make([]string, 0, len(query)+4)
	keys := make([]string, 0, len(query))
	for key := range query {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		values := query[key]
		if len(values) > 0 {
			parts = append(parts, key+"="+values[0])
		}
	}
	if len(body) > 0 {
		digest := md5.Sum(body)
		parts = append(parts, hex.EncodeToString(digest[:]))
	}
	timestamp := strconv.FormatInt(c.now().UnixMilli(), 10)
	randomValue, err := randomAlphaNumeric(c.random, 8)
	if err != nil {
		return nil, err
	}
	parts = append(parts, c.sessionKey, timestamp, randomValue)
	digest := sha256.Sum256([]byte(strings.Join(parts, "&")))
	headers.Set("Web-Signature", hex.EncodeToString(digest[:]))
	headers.Set("Web-Random", randomValue)
	headers.Set("Web-Timestamp", timestamp)
	return headers, nil
}

func (c *Client) rewritePath(path string) string {
	if c.sessionKey != "" {
		return strings.Replace(path, "/ai/portal/v", "/ai/portal/wenc/v", 1)
	}
	return path
}

type chatEvent struct {
	ConversationID string `json:"conversation_id"`
	ID             string `json:"id"`
	Status         string `json:"status"`
}

func endpointFromGateway(value Endpoint, fallback string) string {
	if host := strings.TrimSpace(value.PrivateHost); host != "" {
		return strings.TrimRight(host, "/")
	}
	host := strings.TrimSpace(value.IP)
	if host != "" && value.SecurePort != "" {
		return "https://" + host + ":" + value.SecurePort
	}
	if host != "" && value.PlainPort != "" {
		return "http://" + host + ":" + value.PlainPort
	}
	return strings.TrimRight(fallback, "/")
}

func resultError(action string, envelope rawEnvelope) error {
	message := strings.TrimSpace(envelope.ResultMsg)
	if message == "" {
		message = "未知业务错误"
	}
	return fmt.Errorf("eai: %s失败: code=%s: %s", action, envelope.ResultCode, message)
}

func randomAlphaNumeric(source io.Reader, length int) (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	raw := make([]byte, length)
	if _, err := io.ReadFull(source, raw); err != nil {
		return "", fmt.Errorf("eai: 生成随机数: %w", err)
	}
	result := make([]byte, length)
	for i, value := range raw {
		result[i] = alphabet[int(value)%len(alphabet)]
	}
	return string(result), nil
}

func randomUUID(source io.Reader) (string, error) {
	var raw [16]byte
	if _, err := io.ReadFull(source, raw[:]); err != nil {
		return "", fmt.Errorf("eai: 生成 UUID: %w", err)
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(raw[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32], nil
}
