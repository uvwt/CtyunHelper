package clink

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestInitialPayloadStartsWithREDQ(t *testing.T) {
	payload := InitialPayload()
	if !IsREDQ(payload) {
		t.Fatalf("initial payload = %x", payload[:4])
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

func TestBuildUserInfoMessageUsesType118(t *testing.T) {
	buf, err := BuildUserInfoMessage(123, "user")
	if err != nil {
		t.Fatal(err)
	}
	if got := int(buf[0]) | int(buf[1])<<8; got != 118 {
		t.Fatalf("type = %d", got)
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
