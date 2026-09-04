package auth

import (
	"strings"
	"testing"
)

func TestLoginPasswordMatchesNativeClient(t *testing.T) {
	got := LoginPassword("password", "salt")
	want := "92d690d4eb4a598d5362f7196dba110e3974a9ea58eb9363be73e987d738afc6"
	if got != want {
		t.Fatalf("LoginPassword() = %s, want %s", got, want)
	}
}

func TestPublicSignatureMatchesNativeGetPublicParams(t *testing.T) {
	profile := Profile{UserID: 123, TenantID: 456, SecretKey: "secret-xyz"}
	ctx := RequestContext{RequestID: "1700000000001", Timestamp: "1700000000000"}
	got := PublicSignature(ctx, profile)
	want := stringsUpperSHA256("25" + ctx.RequestID + "456" + ctx.Timestamp + "123" + NativeVersion + "secret-xyz")
	if got != want {
		t.Fatalf("PublicSignature() = %s, want %s", got, want)
	}
}

func TestServerNodeSignatureNormalizesPath(t *testing.T) {
	profile := Profile{UserEID: "eid-abc", SecretKey: "secret-xyz"}
	ctx := RequestContext{
		RequestID: "1700000000001",
		Timestamp: "1700000000000",
		Path:      "api/cdserv/client/device/getSmsCode?x=1",
	}
	got := ServerNodeSignature(ctx, profile, "node-1")
	want := stringsUpperSHA256("25" + ctx.RequestID + ctx.Timestamp + "eid-abc" + NativeVersion + "node-1/api/cdserv/client/device/getSmsCodesecret-xyz")
	if got != want {
		t.Fatalf("ServerNodeSignature() = %s, want %s", got, want)
	}
}

func stringsUpperSHA256(value string) string {
	return strings.ToUpper(SHA256Hex(value))
}
