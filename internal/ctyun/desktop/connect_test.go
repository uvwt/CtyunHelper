package desktop

import (
	"encoding/binary"
	"testing"
)

func TestSessionIdentityBufferLayout(t *testing.T) {
	info := ConnectionInfo{
		DesktopID:           7,
		Token:               "token",
		TenantMemberAccount: "member",
	}
	buf, err := info.SessionIdentityBuffer("device")
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(buf[:4]); got != 7 {
		t.Fatalf("desktopId = %d", got)
	}
	// Token 长度 6（含 NUL），首段正文从 36 开始。
	if got := binary.LittleEndian.Uint32(buf[4:8]); got != 6 {
		t.Fatalf("token length = %d", got)
	}
	if got := binary.LittleEndian.Uint32(buf[8:12]); got != 36 {
		t.Fatalf("token offset = %d", got)
	}
	if got := string(buf[36 : 36+6]); got != "token\x00" {
		t.Fatalf("token body = %q", got)
	}
}
