package points

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uvwt/CtyunHelper/internal/ctyun/auth"
)

func TestPlaceOrderPostsExactRedeemPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/selforder/api/selforder/paas/placeOrder" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var request OrderRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.BusinessChannel != "010" || request.OrderType != 1 || request.PointType != 1 || request.Points != 600 || len(request.SKUs) != 2 {
			t.Fatalf("request = %#v", request)
		}
		if request.SKUs[0].Attributes[0].Key != "bindDesktopId" || request.SKUs[0].Attributes[0].Value != 42 {
			t.Fatalf("attrs = %#v", request.SKUs[0].Attributes)
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"ok": true}})
	}))
	defer server.Close()

	authClient := auth.NewClient(auth.DeviceIdentity{Code: "device"}, auth.ClientOptions{Random: strings.NewReader(strings.Repeat("z", 512))})
	authClient.UseProfile(auth.Profile{UserID: 1, UserEID: "e", TenantID: 2, SecretKey: "s", CommonLoginReqHeader: "c"})
	client := NewClient(authClient, ClientOptions{Origin: server.URL})
	request := OrderRequest{
		BusinessChannel: "010", OrderType: 1, PointType: 1, Points: 600,
		SKUs: []OrderSKU{
			{ExecutionOrder: 1, ProductID: 99, ProductType: "gift", Attributes: []OrderAttr{{Key: "bindDesktopId", Value: 42}}},
			{ExecutionOrder: 2, ProductID: 99, ProductType: "gift", Attributes: []OrderAttr{{Key: "bindDesktopId", Value: 42}}},
		},
	}
	if _, err := client.PlaceOrder(context.Background(), request); err != nil {
		t.Fatal(err)
	}
}
