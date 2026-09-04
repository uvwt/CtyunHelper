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

func TestDeviceBindingFollowsOfficialFlow(t *testing.T) {
	fixedNow := time.UnixMilli(1700000000000)
	profile := Profile{
		UserID: 123, UserEID: "eid", TenantID: 456,
		SecretKey: "test-key", CommonLoginReqHeader: "common",
		MobilePhone: "mobile", BondedDevice: false,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/cdserv/client/getServData":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"serverNodeId": "node"}})
		case "/api/auth/client/validateCode/captcha":
			assertBindingSignature(t, r, profile, "node")
			w.Header().Set("CTG-CAPTCHA-KEY", "captcha-key")
			_, _ = w.Write([]byte(strings.Repeat("x", 128)))
		case "/api/cdserv/client/device/getSmsCode":
			assertBindingSignature(t, r, profile, "node")
			query := r.URL.Query()
			if query.Get("mobilePhone") != "mobile" || query.Get("captchaCode") != "4321" || query.Get("captchaCodeKey") != "captcha-key" {
				t.Fatalf("sms query = %v", query)
			}
			w.Header().Set("CTG-SMS-KEY", "sms-key")
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": true})
		case "/api/cdserv/client/device/binding":
			assertBindingSignature(t, r, profile, "node")
			raw, _ := io.ReadAll(r.Body)
			form, err := url.ParseQuery(string(raw))
			if err != nil {
				t.Fatal(err)
			}
			if form.Get("verificationCode") != "567890" || form.Get("smsCodeKey") != "sms-key" || form.Get("deviceCode") != "ctyun_fixed" {
				t.Fatalf("binding form = %v", form)
			}
			if form.Get("appVersion") != NativeAppVersion || form.Get("deviceInfo") != "windows" || form.Get("hostName") == "" {
				t.Fatalf("native device form = %v", form)
			}
			if _, ok := form["snCode"]; !ok {
				t.Fatalf("snCode must be present: %v", form)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(DeviceIdentity{Code: "ctyun_fixed"}, ClientOptions{
		APIOrigin: server.URL, HTTPClient: server.Client(),
		Now:    func() time.Time { return fixedNow },
		Random: strings.NewReader(strings.Repeat("r", 8192)),
	})
	client.UseProfile(profile)
	challenge, err := client.BeginDeviceBinding(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if challenge.CaptchaKey != "captcha-key" || challenge.Mobile != "mobile" || len(challenge.Captcha) != 128 {
		t.Fatalf("challenge = %#v", challenge)
	}
	smsKey, err := client.SendDeviceSMS(context.Background(), "4321", challenge.CaptchaKey)
	if err != nil {
		t.Fatal(err)
	}
	if smsKey != "sms-key" {
		t.Fatalf("sms key = %q", smsKey)
	}
	bound, err := client.BindDevice(context.Background(), "567890", smsKey)
	if err != nil {
		t.Fatal(err)
	}
	if !bound.BondedDevice {
		t.Fatalf("candidate profile bonded = %v", bound.BondedDevice)
	}
	current, ok := client.Profile()
	if !ok || current.BondedDevice {
		t.Fatalf("low-level binding must not commit profile before app persistence: bonded=%v ok=%v", current.BondedDevice, ok)
	}
}

func assertBindingSignature(t *testing.T, r *http.Request, profile Profile, node string) {
	t.Helper()
	ctx := RequestContext{RequestID: r.Header.Get("CTG-REQUESTID"), Timestamp: r.Header.Get("CTG-TIMESTAMP"), Path: r.URL.Path}
	want := ServerNodeSignature(ctx, profile, node)
	if got := r.Header.Get("CTG-SIGNATURE"); got != want {
		t.Fatalf("signature = %q, want %q", got, want)
	}
	if r.Header.Get("CTG-SERVERNODE") != node || r.Header.Get("CTG-COMMON-DATA") != profile.CommonLoginReqHeader {
		t.Fatal("binding auth headers missing")
	}
}
