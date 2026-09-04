package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestNativeLoginHTTPFlow(t *testing.T) {
	fixedNow := time.UnixMilli(1700000000000)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/client/genChallengeData":
			if r.Method != http.MethodPost || r.Header.Get("CTG-DEVICECODE") != "device-code" {
				t.Fatalf("unexpected challenge request")
			}
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"challengeId": "challenge-id", "challengeCode": "salt"}})
		case "/api/auth/client/captcha":
			w.Header().Set("CTG-CAPTCHA-KEY", "captcha-key")
			w.Write([]byte("0123456789abcdefghijklmnop"))
		case "/api/auth/client/login":
			body, _ := io.ReadAll(r.Body)
			form, _ := url.ParseQuery(string(body))
			if got := form.Get("password"); got != LoginPassword("password", "salt") {
				t.Fatalf("password digest = %s", got)
			}
			if form.Get("captchaCodeKey") != "captcha-key" || form.Get("captchaCode") != "4321" {
				t.Fatalf("captcha fields = %v", form)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"userId": 123, "userEid": "eid-abc", "tenantId": 456,
					"secretKey": "secret-xyz", "commonLoginReqHeader": "common-data",
					"bondedDevice": true, "timestamp": fixedNow.UnixMilli(),
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(DeviceIdentity{Code: "device-code"}, ClientOptions{
		APIOrigin:  server.URL,
		HTTPClient: server.Client(),
		Now:        func() time.Time { return fixedNow },
		Random:     strings.NewReader(strings.Repeat("a", 256)),
	})
	challenge, err := client.BeginLogin(context.Background(), "account")
	if err != nil {
		t.Fatal(err)
	}
	if challenge.CaptchaKey != "captcha-key" || len(challenge.Captcha) == 0 {
		t.Fatalf("challenge = %#v", challenge)
	}
	profile, err := client.Login(context.Background(), "account", "password", "4321", challenge)
	if err != nil {
		t.Fatal(err)
	}
	if profile.UserID != 123 || profile.CommonLoginReqHeader != "common-data" {
		t.Fatalf("profile = %#v", profile)
	}
}

func TestGetTicketCarriesNativePublicSignature(t *testing.T) {
	fixedNow := time.UnixMilli(1700000000000)
	profile := Profile{UserID: 123, UserEID: "eid", TenantID: 456, SecretKey: "secret-xyz", CommonLoginReqHeader: "common-data"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := RequestContext{RequestID: r.Header.Get("CTG-REQUESTID"), Timestamp: r.Header.Get("CTG-TIMESTAMP"), Path: r.URL.Path}
		if r.Header.Get("CTG-SIGNATURESTR") != PublicSignature(ctx, profile) {
			t.Fatalf("invalid signature")
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"ticket": "ticket-value"}})
	}))
	defer server.Close()

	client := NewClient(DeviceIdentity{Code: "device-code"}, ClientOptions{
		APIOrigin: server.URL, HTTPClient: server.Client(), Now: func() time.Time { return fixedNow }, Random: strings.NewReader(strings.Repeat("b", 256)),
	})
	client.UseProfile(profile)
	ticket, err := client.GetTicket(context.Background(), "https://example.test")
	if err != nil {
		t.Fatal(err)
	}
	if ticket != "ticket-value" {
		t.Fatalf("ticket = %s", ticket)
	}
}
