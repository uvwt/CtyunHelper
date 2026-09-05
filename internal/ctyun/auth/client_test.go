package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestNativeLoginHTTPFlowStartsWithoutCaptcha(t *testing.T) {
	fixedNow := time.UnixMilli(1700000000000)
	captchaCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/client/genChallengeData":
			if r.Method != http.MethodPost || r.Header.Get("CTG-DEVICECODE") != "device-code" {
				t.Fatalf("unexpected challenge request")
			}
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"challengeId": "challenge-id", "challengeCode": "salt"}})
		case "/api/auth/client/captcha":
			captchaCalls++
			w.Header().Set("CTG-CAPTCHA-KEY", "captcha-key")
			w.Write([]byte("0123456789abcdefghijklmnop"))
		case "/api/auth/client/login":
			body, _ := io.ReadAll(r.Body)
			form, _ := url.ParseQuery(string(body))
			if got := form.Get("password"); got != LoginPassword("password", "salt") {
				t.Fatalf("password digest = %s", got)
			}
			if form.Get("captchaCodeKey") != "" || form.Get("captchaCode") != "" {
				t.Fatalf("normal login must not send captcha fields: %v", form)
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
	if captchaCalls != 0 {
		t.Fatalf("captcha calls before server request = %d", captchaCalls)
	}
	profile, err := client.Login(context.Background(), "account", "password", "", "", challenge)
	if err != nil {
		t.Fatal(err)
	}
	if profile.UserID != 123 || profile.CommonLoginReqHeader != "common-data" {
		t.Fatalf("profile = %#v", profile)
	}
	if _, ok := client.Profile(); ok {
		t.Fatal("low-level Login must not install candidate profile before app persistence succeeds")
	}
}

func TestLoginCaptchaIsFetchedOnlyWhenRequested(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("CTG-CAPTCHA-KEY", "captcha-key")
		w.Write([]byte("0123456789abcdefghijklmnop"))
	}))
	defer server.Close()
	client := NewClient(DeviceIdentity{Code: "device-code"}, ClientOptions{APIOrigin: server.URL, HTTPClient: server.Client()})
	captcha, err := client.GetLoginCaptcha(context.Background(), "account")
	if err != nil {
		t.Fatal(err)
	}
	if captcha.Key != "captcha-key" || len(captcha.Image) == 0 {
		t.Fatalf("captcha = %#v", captcha)
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

func TestAPIErrorClassificationSurvivesWrapping(t *testing.T) {
	wrappedAuth := fmt.Errorf("desktop: %w", APIError{Code: CodeNoPermissions, Message: "expired"})
	if !RequiresAuthentication(wrappedAuth) || RequiresDeviceBinding(wrappedAuth) {
		t.Fatalf("authentication classification failed: %v", wrappedAuth)
	}
	wrappedBind := fmt.Errorf("desktop: %w", APIError{Code: CodeDeviceUnbound, Message: "unbind"})
	if !RequiresDeviceBinding(wrappedBind) || RequiresAuthentication(wrappedBind) {
		t.Fatalf("binding classification failed: %v", wrappedBind)
	}
	for _, code := range []int{CodeNeedCaptcha, CodeEmptyCaptcha, CodeInvalidCaptcha, CodeExpiredCaptcha} {
		wrappedCaptcha := fmt.Errorf("login: %w", APIError{Code: code, Message: "captcha"})
		if !RequiresLoginCaptcha(wrappedCaptcha) {
			t.Fatalf("captcha code %d was not classified", code)
		}
	}
}
