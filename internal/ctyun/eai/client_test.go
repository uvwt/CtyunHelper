package eai

import (
	"context"
	"crypto/aes"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"
)

type ticketStub struct {
	service string
}

func (s *ticketStub) GetTicket(_ context.Context, service string) (string, error) {
	s.service = service
	return "ticket-value", nil
}

func TestEAIEndToEndProtocolFlow(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	publicKey := base64.StdEncoding.EncodeToString(publicDER)
	const sessionKey = "session-key-for-tests"
	fixedNow := time.UnixMilli(1700000000000)
	var authorizeCalls int

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/server/eaiSysInfo":
			gatewayRaw, _ := json.Marshal(map[string]any{
				"eai": map[string]any{"privateHost": server.URL},
				"sso": map[string]any{"ssopk": publicKey, "ssopkid": "kid-1"},
			})
			json.NewEncoder(w).Encode(map[string]any{
				"resultCode": 0,
				"data":       encryptAESECBBase64ForTest(t, gatewayRaw, gatewayAESKey),
			})
		case "/sso/login/v2/iam/ticketAuthorize":
			authorizeCalls++
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("loginType") != "iamTicket" || r.Form.Get("clientId") != "eaiapp" || r.Form.Get("iamTicket") != "ticket-value" || r.Form.Get("clientKeyId") != "kid-1" {
				t.Fatalf("SSO form = %v", r.Form)
			}
			encryptedKey, err := hex.DecodeString(r.Form.Get("clientKey"))
			if err != nil {
				t.Fatal(err)
			}
			clientKey, err := rsa.DecryptPKCS1v15(rand.Reader, privateKey, encryptedKey)
			if err != nil {
				t.Fatal(err)
			}
			if len(clientKey) != 16 {
				t.Fatalf("clientKey len = %d", len(clientKey))
			}
			http.SetCookie(w, &http.Cookie{Name: "eai-session", Value: "ok", Path: "/"})
			json.NewEncoder(w).Encode(map[string]any{
				"resultCode": "0",
				"data": map[string]any{
					"sessionKey": encryptAESECBBase64ForTest(t, []byte(sessionKey), clientKey),
				},
			})
		case "/ai/portal/wenc/v2/user/queryUserTenantInfo":
			cookie, err := r.Cookie("eai-session")
			if err != nil || cookie.Value != "ok" {
				t.Fatalf("SSO cookie missing: cookie=%v err=%v", cookie, err)
			}
			assertWebSignature(t, r, sessionKey, nil)
			json.NewEncoder(w).Encode(map[string]any{
				"resultCode": "0",
				"data":       map[string]any{"data": []map[string]any{{"tenantId": 42, "tenantIdStr": "tenant-42"}}},
			})
		case "/ai/portal/wenc/v2/openai/chat/queryModels":
			if r.URL.Query().Get("type") != "all" {
				t.Fatalf("models query = %v", r.URL.Query())
			}
			assertWebSignature(t, r, sessionKey, nil)
			if r.Header.Get("x-eai-tenant-id") != "tenant-42" {
				t.Fatalf("tenant header = %q", r.Header.Get("x-eai-tenant-id"))
			}
			json.NewEncoder(w).Encode(map[string]any{
				"resultCode": "0",
				"data": []map[string]any{
					{"keyModel": "fallback", "status": "offline", "type": "text"},
					{"keyModel": "model-good", "status": "avaiable", "type": "chat"},
				},
			})
		case "/ai/portal/wenc/v3/openai/chat/completions":
			raw, _ := io.ReadAll(r.Body)
			assertWebSignature(t, r, sessionKey, raw)
			var body struct {
				KeyModel    string `json:"key_model"`
				ClientRetry bool   `json:"client_retry"`
				TenantID    int64  `json:"tenantId"`
				Messages    []struct {
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatal(err)
			}
			if body.KeyModel != "model-good" || body.ClientRetry || body.TenantID != 42 || len(body.Messages) != 1 || body.Messages[0].Content != "你好" {
				t.Fatalf("chat body = %#v", body)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "data: {\"status\":\"generating\",\"conversation_id\":\"c1\"}\n\n")
			io.WriteString(w, "data: {\"status\":\"finish\",\"conversation_id\":\"c1\"}\n\n")
			io.WriteString(w, "data: [DONE]\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tickets := &ticketStub{}
	client, err := NewClient(tickets, ClientOptions{
		GatewayURL: server.URL + "/server/eaiSysInfo",
		HTTPClient: server.Client(),
		Now:        func() time.Time { return fixedNow },
		Random:     strings.NewReader(strings.Repeat("r", 32768)),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.SendMessage(context.Background(), "你好")
	if err != nil {
		t.Fatal(err)
	}
	if tickets.service != ServiceURL {
		t.Fatalf("ticket service = %q", tickets.service)
	}
	if result.Model != "model-good" || result.TenantID != 42 || result.ConversationID != "c1" || result.EventCount != 2 || !result.Finished {
		t.Fatalf("result = %#v", result)
	}
	second, err := client.SendMessage(context.Background(), "你好")
	if err != nil {
		t.Fatal(err)
	}
	if second.Model != "model-good" || authorizeCalls != 2 {
		t.Fatalf("second result = %#v, authorize calls = %d", second, authorizeCalls)
	}
}

func TestWebSignatureMatchesFrontendFormula(t *testing.T) {
	client, err := NewClient(&ticketStub{}, ClientOptions{
		Now:    func() time.Time { return time.UnixMilli(1700000000000) },
		Random: strings.NewReader(strings.Repeat("A", 512)),
	})
	if err != nil {
		t.Fatal(err)
	}
	client.sessionKey = "session-key"
	body := []byte(`{"x":1}`)
	headers, err := client.signedHeaders(url.Values{"b": {"2"}, "a": {"1"}}, body, "tenant")
	if err != nil {
		t.Fatal(err)
	}
	md5Digest := md5.Sum(body)
	origin := "a=1&b=2&" + hex.EncodeToString(md5Digest[:]) + "&session-key&1700000000000&"
	randomValue := headers.Get("Web-Random")
	want := sha256.Sum256([]byte(origin + randomValue))
	if headers.Get("Web-Signature") != hex.EncodeToString(want[:]) {
		t.Fatalf("signature = %q", headers.Get("Web-Signature"))
	}
	if headers.Get("Web-Timestamp") != "1700000000000" || headers.Get("x-eai-tenant-id") != "tenant" {
		t.Fatalf("headers = %v", headers)
	}
}

func TestParseSSEIgnoresInvalidAndDoneLines(t *testing.T) {
	events, err := parseSSE(strings.NewReader("event: message\ndata: invalid\ndata: {\"status\":\"finish\",\"id\":\"x\"}\ndata: [DONE]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != "x" || events[0].Status != "finish" {
		t.Fatalf("events = %#v", events)
	}
}

func encryptAESECBBase64ForTest(t *testing.T, plain, key []byte) string {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	padding := block.BlockSize() - len(plain)%block.BlockSize()
	padded := append(append([]byte(nil), plain...), bytesRepeat(byte(padding), padding)...)
	encrypted := make([]byte, len(padded))
	for offset := 0; offset < len(padded); offset += block.BlockSize() {
		block.Encrypt(encrypted[offset:offset+block.BlockSize()], padded[offset:offset+block.BlockSize()])
	}
	return base64.StdEncoding.EncodeToString(encrypted)
}

func bytesRepeat(value byte, count int) []byte {
	result := make([]byte, count)
	for i := range result {
		result[i] = value
	}
	return result
}

func assertWebSignature(t *testing.T, r *http.Request, sessionKey string, body []byte) {
	t.Helper()
	parts := make([]string, 0, len(r.URL.Query())+4)
	keys := make([]string, 0, len(r.URL.Query()))
	for key := range r.URL.Query() {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, key+"="+r.URL.Query().Get(key))
	}
	if len(body) > 0 {
		digest := md5.Sum(body)
		parts = append(parts, hex.EncodeToString(digest[:]))
	}
	parts = append(parts, sessionKey, r.Header.Get("Web-Timestamp"), r.Header.Get("Web-Random"))
	digest := sha256.Sum256([]byte(strings.Join(parts, "&")))
	if got, want := r.Header.Get("Web-Signature"), hex.EncodeToString(digest[:]); got != want {
		t.Fatalf("Web-Signature = %q, want %q", got, want)
	}
	if r.Header.Get("x-client-trace-id") == "" || r.Header.Get("YL-Main-Version") != Version || r.Header.Get("YL-Product-Id") != ProductID {
		t.Fatalf("EAI headers incomplete: %v", r.Header)
	}
}
