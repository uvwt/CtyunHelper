package points

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/uvwt/CtyunHelper/internal/ctyun/auth"
)

func TestPointsClientUsesNativePublicHeadersAndMergesDesktops(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("CTG-SIGNATURESTR") == "" || r.Header.Get("CTG-COMMON-DATA") != "common-data" {
			t.Fatalf("missing native headers: %v", r.Header)
		}
		if r.Header.Get("From") != "App-web" {
			t.Fatalf("From = %q", r.Header.Get("From"))
		}
		switch r.URL.Path {
		case "/selforder/api/desktop/client/pageDesktop":
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
				"desktopList":           []map[string]any{{"desktopId": 1, "desktopName": "A"}},
				"desktopPoolList":       []map[string]any{{"desktopId": 2, "desktopName": "B"}},
				"preemptionDesktopList": []map[string]any{{"desktopId": 3, "desktopName": "C"}},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	authClient := auth.NewClient(auth.DeviceIdentity{Code: "device"}, auth.ClientOptions{
		Now:    func() time.Time { return time.UnixMilli(1700000000000) },
		Random: strings.NewReader(strings.Repeat("x", 512)),
	})
	authClient.UseProfile(auth.Profile{
		UserID: 123, UserEID: "eid", TenantID: 456, SecretKey: "secret", CommonLoginReqHeader: "common-data",
	})
	client := NewClient(authClient, ClientOptions{Origin: server.URL})
	desktops, err := client.Desktops(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(desktops) != 3 || desktops[2].Name() != "C" {
		t.Fatalf("desktops = %#v", desktops)
	}
}

func TestGeneralPointsUsesPointsFieldFromRealResponseShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": []map[string]any{
			{"pointType": 1, "points": 650, "pretakePoints": 0},
			{"pointType": 2, "points": 999, "pretakePoints": 0},
		}})
	}))
	defer server.Close()

	authClient := auth.NewClient(auth.DeviceIdentity{Code: "device"}, auth.ClientOptions{Random: strings.NewReader(strings.Repeat("y", 512))})
	authClient.UseProfile(auth.Profile{UserID: 1, UserEID: "e", TenantID: 2, SecretKey: "s", CommonLoginReqHeader: "c"})
	client := NewClient(authClient, ClientOptions{Origin: server.URL})
	points, err := client.GeneralPoints(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if points != 650 {
		t.Fatalf("points = %d", points)
	}
}
