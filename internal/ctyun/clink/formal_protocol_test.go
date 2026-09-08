package clink

import (
	"encoding/binary"
	"testing"
)

func TestBuildClientLoginMessageMatchesLegacyCtYunLayout(t *testing.T) {
	buf, err := BuildClientLoginMessage(7, "token", "device", "account")
	if err != nil {
		t.Fatal(err)
	}
	messages, err := ParseMessages(buf)
	if err != nil || len(messages) != 1 || messages[0].Type != msgMainClientLogin {
		t.Fatalf("login message=%#v err=%v", messages, err)
	}
	payload := messages[0].Data
	if got := binary.LittleEndian.Uint32(payload[0:4]); got != 7 {
		t.Fatalf("desktopId=%d", got)
	}
	wantFields := []string{"token", "60", "device", "account"}
	for index, want := range wantFields {
		base := 4 + index*8
		length := binary.LittleEndian.Uint32(payload[base : base+4])
		offset := binary.LittleEndian.Uint32(payload[base+4 : base+8])
		if length != uint32(len(want)+1) {
			t.Fatalf("field %d length=%d", index, length)
		}
		got := string(payload[offset : offset+length-1])
		if got != want || payload[offset+length-1] != 0 {
			t.Fatalf("field %d=%q nul=%d", index, got, payload[offset+length-1])
		}
	}
	if got := binary.LittleEndian.Uint32(payload[8:12]); got != 36 {
		t.Fatalf("token offset=%d", got)
	}
}

func TestFormalControlMessagesUseRawFraming(t *testing.T) {
	for _, test := range []struct {
		name string
		buf  []byte
		want uint16
	}{
		{name: "attach", buf: BuildAttachChannelsMessage(), want: msgMainAttach},
		{name: "client version", buf: BuildClientVersionMessage(), want: msgMainClientVersion},
		{name: "heartbeat", buf: BuildHeartbeatMessage(), want: msgHeartbeat},
	} {
		t.Run(test.name, func(t *testing.T) {
			messages, err := ParseMessages(test.buf)
			if err != nil || len(messages) != 1 || messages[0].Type != test.want || len(messages[0].Data) != 0 {
				t.Fatalf("message=%#v err=%v", messages, err)
			}
		})
	}
}

func TestParseLoginResult(t *testing.T) {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, 23)
	got, err := ParseLoginResult(data)
	if err != nil || got != 23 {
		t.Fatalf("result=%d err=%v", got, err)
	}
}
