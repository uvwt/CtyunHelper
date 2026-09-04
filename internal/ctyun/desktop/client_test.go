package desktop

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uvwt/CtyunHelper/internal/ctyun/auth"
)

func TestDesktopDiscoveryAndConnectUseHostBoundServerNode(t *testing.T) {
	fixedNow := time.UnixMilli(1700000000000)
	profile := auth.Profile{
		UserID:               123,
		UserEID:              "eid-abc",
		TenantID:             456,
		SecretKey:            "secret-xyz",
		CommonLoginReqHeader: "common-data",
	}
	var regionServerDataCalls atomic.Int32

	var region *httptest.Server
	region = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/cdserv/client/getServData":
			regionServerDataCalls.Add(1)
			json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"serverNodeId": "region-node", "timestamp": fixedNow.UnixMilli()},
			})
		case queryConnectDataPath, connectPath:
			assertServerNodeSignature(t, r, profile, "region-node")
			body, _ := io.ReadAll(r.Body)
			form, err := url.ParseQuery(string(body))
			if err != nil {
				t.Fatal(err)
			}
			if form.Get("objId") != "7" || form.Get("osType") != "15" || form.Get("deviceId") != "25" {
				t.Fatalf("connect params = %v", form)
			}
			if form.Get("hardwareFeatureCode") != "device-code" || !strings.Contains(form.Get("vdCommand"), `"command":9`) {
				t.Fatalf("device params = %v", form)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"goingRetry": false,
					"desktopInfo": map[string]any{
						"desktopId":       7,
						"host":            "desktop.internal",
						"port":            "7033",
						"clinkLvsOutHost": "127.0.0.1:8011",
						"caCert":          "ca",
						"clientCert":      "cert",
						"clientKey":       "key",
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer region.Close()

	master := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/cdserv/client/getServData":
			json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"serverNodeId": "master-node", "timestamp": fixedNow.UnixMilli()},
			})
		case pageDesktopPath:
			assertServerNodeSignature(t, r, profile, "master-node")
			var request struct {
				DesktopTypes []string `json:"desktopTypes"`
				Count        int      `json:"getCnt"`
				SortType     string   `json:"sortType"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if request.Count != 30 || request.SortType != "createTimeV1" || strings.Join(request.DesktopTypes, ",") != "1,2001,2002" {
				t.Fatalf("page request = %#v", request)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"desktopList": []map[string]any{{
						"objType": 0, "objId": "7", "desktopId": "7", "desktopName": "测试云电脑",
						"useStatus": "25", "useStatusText": "运行中", "backupurl": []string{region.URL},
					}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer master.Close()

	authClient := auth.NewClient(auth.DeviceIdentity{Code: "device-code"}, auth.ClientOptions{
		APIOrigin:  master.URL,
		HTTPClient: master.Client(),
		Now:        func() time.Time { return fixedNow },
		Random:     strings.NewReader(strings.Repeat("x", 4096)),
	})
	// master.Client 的 Transport 同样可以访问另一个 httptest server。
	authClient.UseProfile(profile)
	client := NewClient(authClient, ClientOptions{MasterOrigin: master.URL})
	values, err := client.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].ID() != "7" || !values[0].Running() {
		t.Fatalf("desktops = %#v", values)
	}
	connection, err := client.ResolveConnection(context.Background(), values[0])
	if err != nil {
		t.Fatal(err)
	}
	if connection.DesktopID != 7 || connection.ClinkLVSOutHost != "127.0.0.1:8011" || connection.ClientCert != "cert" {
		t.Fatalf("connection = %#v", connection)
	}
	if got := regionServerDataCalls.Load(); got != 1 {
		t.Fatalf("region getServData calls = %d, want 1", got)
	}
}

func TestEncryptedDesktopResponseIsRejectedExplicitly(t *testing.T) {
	fixedNow := time.UnixMilli(1700000000000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/cdserv/client/getServData" {
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"serverNodeId": "node"}})
			return
		}
		w.Header().Set("CTG-RSPDATA-ETYPE", "2")
		json.NewEncoder(w).Encode(map[string]any{"edata": "encrypted"})
	}))
	defer server.Close()
	authClient := auth.NewClient(auth.DeviceIdentity{Code: "device-code"}, auth.ClientOptions{
		APIOrigin: server.URL, HTTPClient: server.Client(), Now: func() time.Time { return fixedNow }, Random: strings.NewReader(strings.Repeat("y", 4096)),
	})
	authClient.UseProfile(auth.Profile{UserID: 1, UserEID: "eid", TenantID: 2, SecretKey: "secret", CommonLoginReqHeader: "common"})
	client := NewClient(authClient, ClientOptions{MasterOrigin: server.URL})
	_, err := client.List(context.Background())
	if err == nil || !strings.Contains(err.Error(), "加密响应类型 2") {
		t.Fatalf("error = %v", err)
	}
}

func assertServerNodeSignature(t *testing.T, r *http.Request, profile auth.Profile, node string) {
	t.Helper()
	ctx := auth.RequestContext{
		RequestID: r.Header.Get("CTG-REQUESTID"),
		Timestamp: r.Header.Get("CTG-TIMESTAMP"),
		Path:      r.URL.Path,
	}
	want := auth.ServerNodeSignatureWithIdentity(auth.WindowsIdentity(), ctx, profile, node)
	if got := r.Header.Get("CTG-SIGNATURE"); got != want {
		t.Fatalf("signature = %s, want %s", got, want)
	}
	if r.Header.Get("CTG-SERVERNODE") != node || r.Header.Get("CTG-USEREID") != profile.UserEID {
		t.Fatalf("headers missing server node auth")
	}
}

func TestNodeSignatureFailureInvalidatesHostCache(t *testing.T) {
	fixedNow := time.UnixMilli(1700000000000)
	profile := auth.Profile{UserID: 1, UserEID: "eid", TenantID: 2, SecretKey: "secret", CommonLoginReqHeader: "common"}
	var serverDataCalls atomic.Int32
	var pageCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/cdserv/client/getServData":
			call := serverDataCalls.Add(1)
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"serverNodeId": "node-" + strconv.Itoa(int(call))}})
		case pageDesktopPath:
			call := pageCalls.Add(1)
			if call == 1 {
				json.NewEncoder(w).Encode(map[string]any{"code": auth.CodeNodeSignatureMismatch, "msg": "node changed"})
				return
			}
			if r.Header.Get("CTG-SERVERNODE") != "node-2" {
				t.Fatalf("second node = %q", r.Header.Get("CTG-SERVERNODE"))
			}
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"desktopList": []any{}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	authClient := auth.NewClient(auth.DeviceIdentity{Code: "device-code"}, auth.ClientOptions{
		APIOrigin: server.URL, HTTPClient: server.Client(), Now: func() time.Time { return fixedNow }, Random: strings.NewReader(strings.Repeat("z", 4096)),
	})
	authClient.UseProfile(profile)
	client := NewClient(authClient, ClientOptions{MasterOrigin: server.URL})
	if _, err := client.List(context.Background()); err == nil {
		t.Fatal("first List() expected node signature error")
	}
	if _, err := client.List(context.Background()); err != nil {
		t.Fatalf("second List() = %v", err)
	}
	if got := serverDataCalls.Load(); got != 2 {
		t.Fatalf("getServData calls = %d, want 2", got)
	}
}
