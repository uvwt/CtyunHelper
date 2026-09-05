package auth

import (
	"context"
	"crypto/md5" //nolint:gosec // test vector for the legacy CtYun protocol
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestLegacyClinkLoginAndAuthenticatedHeaders(t *testing.T) {
	fixedNow := time.UnixMilli(1700000000000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/client/genChallengeData":
			assertLegacyClinkBaseHeaders(t, r)
			json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"challengeId": "challenge-id", "challengeCode": "salt"},
			})
		case "/api/auth/client/login":
			assertLegacyClinkBaseHeaders(t, r)
			body, _ := io.ReadAll(r.Body)
			form, err := url.ParseQuery(string(body))
			if err != nil {
				t.Fatal(err)
			}
			if form.Get("password") != SHA256Hex("password"+"salt") {
				t.Fatalf("password field does not match legacy protocol")
			}
			if form.Get("sha256Password") != LoginPassword("password", "salt") {
				t.Fatalf("sha256Password field does not match legacy protocol")
			}
			if form.Get("deviceType") != "60" || form.Get("clientVersion") != "103020001" || form.Get("appVersion") != "3.2.0" {
				t.Fatalf("legacy login identity = %v", form)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"userId": 123, "userEid": "eid", "tenantId": 456,
					"secretKey": "secret", "commonLoginReqHeader": "common",
					"userName": "tester", "bondedDevice": true,
					"timestamp": fixedNow.UnixMilli(),
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
	})
	profile, err := client.LoginLegacyClink(context.Background(), "account", "password")
	if err != nil {
		t.Fatal(err)
	}
	if profile.UserID != 123 || profile.TenantID != 456 || !profile.BondedDevice {
		t.Fatalf("profile = %#v", profile)
	}
	client.UseProfile(profile)
	headers, err := client.LegacyClinkHeaders()
	if err != nil {
		t.Fatal(err)
	}
	timestamp := headers.Get("ctg-timestamp")
	if timestamp != "1700000000000" || headers.Get("ctg-requestid") != timestamp {
		t.Fatalf("legacy timestamp headers = %v", headers)
	}
	source := "60" + timestamp + "456" + timestamp + "123" + "103020001" + "secret"
	digest := md5.Sum([]byte(source))
	if got, want := headers.Get("ctg-signaturestr"), hex.EncodeToString(digest[:]); got != want {
		t.Fatalf("legacy signature = %q, want %q", got, want)
	}
}

func assertLegacyClinkBaseHeaders(t *testing.T, r *http.Request) {
	t.Helper()
	if r.Header.Get("ctg-devicetype") != "60" || r.Header.Get("ctg-version") != "103020001" || r.Header.Get("ctg-devicecode") != "device-code" {
		t.Fatalf("legacy headers = %v", r.Header)
	}
	if r.Header.Get("Referer") != "https://pc.ctyun.cn/" || !strings.Contains(r.Header.Get("User-Agent"), "Chrome/137") {
		t.Fatalf("legacy browser headers = %v", r.Header)
	}
}
