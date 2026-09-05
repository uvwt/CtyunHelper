package clink

import "testing"

func TestParseProxyServer(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		scheme string
		want   string
	}{
		{name: "generic", value: "127.0.0.1:10808", scheme: "wss", want: "http://127.0.0.1:10808"},
		{name: "https mapping", value: "http=127.0.0.1:8080;https=127.0.0.1:8443", scheme: "wss", want: "http://127.0.0.1:8443"},
		{name: "preserve scheme", value: "socks5://127.0.0.1:1080", scheme: "wss", want: "socks5://127.0.0.1:1080"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseProxyServer(tt.value, tt.scheme)
			if err != nil {
				t.Fatal(err)
			}
			if got == nil || got.String() != tt.want {
				t.Fatalf("proxy=%v, want %s", got, tt.want)
			}
		})
	}
}
