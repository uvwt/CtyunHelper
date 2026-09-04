package clink

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"reflect"
	"testing"
)

func TestInitialPayloadMatchesCtYunVector(t *testing.T) {
	payload := InitialPayload()
	const expectedHex = "5245445102000000020000001a0000000000000001000100000001000000120000000900000004080000"
	expected, err := hex.DecodeString(expectedHex)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(payload, expected) {
		t.Fatalf("initial payload = %x, want %x", payload, expected)
	}
}

func TestProxyHandshakeMatchesCtYunFields(t *testing.T) {
	got := NewProxyHandshake(
		"clink.example.test:9443", "desktop.internal", "443",
		"ca", "cert", "key",
	)
	want := ProxyHandshake{
		Type: 1, SSL: 1, Host: "clink.example.test", Port: "9443",
		CA: "ca", Cert: "cert", Key: "key",
		ServerName: "desktop.internal:443", OQS: 0,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("handshake = %#v, want %#v", got, want)
	}
}

func TestMessageRoundTrip(t *testing.T) {
	original := Message{Type: 103, Data: []byte("abc")}
	buf := original.Marshal(false)
	messages, err := ParseMessages(buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Type != 103 || !bytes.Equal(messages[0].Data, []byte("abc")) {
		t.Fatalf("parsed = %#v", messages)
	}
}

func TestParseMessagesKeepsValidPrefixBeforeBrokenTail(t *testing.T) {
	buf := (Message{Type: 103, Data: []byte("request")}).Marshal(false)
	buf = append(buf, 1, 2, 3)
	messages, err := ParseMessages(buf)
	if err == nil {
		t.Fatal("expected broken-tail parse error")
	}
	if len(messages) != 1 || messages[0].Type != 103 || !bytes.Equal(messages[0].Data, []byte("request")) {
		t.Fatalf("valid prefix was lost: %#v", messages)
	}
}

func TestBuildUserInfoMessageUsesType118(t *testing.T) {
	buf, err := BuildUserInfoMessage(123, "user")
	if err != nil {
		t.Fatal(err)
	}
	const payload = `{"type":1,"userName":"user","userInfo":"","userId":123}`
	if got := binary.LittleEndian.Uint16(buf[0:2]); got != 118 {
		t.Fatalf("type = %d", got)
	}
	if got := binary.LittleEndian.Uint32(buf[2:6]); got != uint32(8+len(payload)) {
		t.Fatalf("outer size = %d", got)
	}
	if got := binary.LittleEndian.Uint32(buf[6:10]); got != uint32(len(payload)) {
		t.Fatalf("payload size = %d", got)
	}
	if got := binary.LittleEndian.Uint32(buf[10:14]); got != 8 {
		t.Fatalf("build offset = %d", got)
	}
	if got := string(buf[14:]); got != payload {
		t.Fatalf("payload = %q", got)
	}
}

func TestREDQResponseMatchesIndependentVector(t *testing.T) {
	challenge := make([]byte, 182)
	copy(challenge, []byte("REDQ"))
	challenge[48] = 0
	for i := 49; i < 177; i++ {
		challenge[i] = 0xff
	}
	challenge[176] = 0xc5
	challenge[179], challenge[180], challenge[181] = 1, 0, 1

	seed := bytes.NewReader([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19})
	response, err := buildREDQResponse(challenge, seed)
	if err != nil {
		t.Fatal(err)
	}
	if len(response) != 132 {
		t.Fatalf("response length = %d", len(response))
	}
	digest := sha256.Sum256(response)
	got := hex.EncodeToString(digest[:])
	const want = "5c68887672231fb4675559a9dda74e74d3d08108ee9562b0895cf99127aaff69"
	if got != want {
		t.Fatalf("REDQ response sha256 = %s, want %s", got, want)
	}
}
