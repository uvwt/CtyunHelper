//go:build windows

package clink

import (
	"testing"

	"golang.org/x/sys/windows"
)

func TestPickPhysicalIPv4InterfaceSkipsTunAndVirtualAdapters(t *testing.T) {
	candidates := []adapterCandidate{
		{
			ifIndex:      1,
			ifType:       ifTypeLoopback,
			operStatus:   windows.IfOperStatusUp,
			friendlyName: "Loopback Pseudo-Interface 1",
			description:  "Software Loopback Interface 1",
			hasGateway:   false,
			ipv4Metric:   75,
		},
		{
			ifIndex:      6,
			ifType:       ifTypePropVirtual,
			operStatus:   windows.IfOperStatusUp,
			friendlyName: "Tailscale",
			description:  "Tailscale Tunnel",
			hasGateway:   false,
			ipv4Metric:   5,
		},
		{
			ifIndex:      19,
			ifType:       ifTypePropVirtual,
			operStatus:   windows.IfOperStatusUp,
			friendlyName: "singbox_tun",
			description:  "Wintun Userspace Tunnel",
			hasGateway:   true,
			ipv4Metric:   1,
		},
		{
			ifIndex:      21,
			ifType:       ifTypeEthernetCSMACD,
			operStatus:   windows.IfOperStatusUp,
			friendlyName: "Clash TUN",
			description:  "Wintun Userspace Tunnel",
			hasGateway:   true,
			ipv4Metric:   2,
		},
		{
			ifIndex:      14,
			ifType:       ifTypeIEEE80211,
			operStatus:   windows.IfOperStatusUp,
			friendlyName: "WLAN",
			description:  "Intel Wi-Fi 6",
			hasGateway:   true,
			ipv4Metric:   10,
		},
		{
			ifIndex:      11,
			ifType:       ifTypeEthernetCSMACD,
			operStatus:   windows.IfOperStatusUp,
			friendlyName: "以太网",
			description:  "Red Hat VirtIO Ethernet Adapter",
			hasGateway:   true,
			ipv4Metric:   15,
		},
		{
			ifIndex:      12,
			ifType:       ifTypeEthernetCSMACD,
			operStatus:   windows.IfOperStatusDown,
			friendlyName: "以太网 2",
			description:  "Disconnected NIC",
			hasGateway:   true,
			ipv4Metric:   5,
		},
	}

	got, ok := pickPhysicalIPv4Interface(candidates)
	if !ok {
		t.Fatal("expected physical interface to be selected")
	}
	if got != 11 {
		t.Fatalf("selected ifIndex=%d, want 11 (ethernet)", got)
	}
}

func TestPickPhysicalIPv4InterfaceReturnsFalseWhenNoneAvailable(t *testing.T) {
	candidates := []adapterCandidate{
		{
			ifIndex:      19,
			ifType:       ifTypePropVirtual,
			operStatus:   windows.IfOperStatusUp,
			friendlyName: "singbox_tun",
			description:  "Wintun Userspace Tunnel",
			hasGateway:   true,
			ipv4Metric:   1,
		},
		{
			ifIndex:      11,
			ifType:       ifTypeEthernetCSMACD,
			operStatus:   windows.IfOperStatusUp,
			friendlyName: "以太网",
			description:  "Red Hat VirtIO Ethernet Adapter",
			hasGateway:   false,
			ipv4Metric:   15,
		},
	}

	if got, ok := pickPhysicalIPv4Interface(candidates); ok {
		t.Fatalf("expected no interface selected, got %d", got)
	}
}

func TestShouldPreferPhysicalInterface(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{addr: "10.36.177.125:9011", want: true},
		{addr: "desk.ctyun.cn:8810", want: true},
		{addr: "127.0.0.1:10808", want: false},
		{addr: "localhost:9011", want: false},
		{addr: "[::1]:9011", want: false},
		{addr: "[240e::1]:9011", want: false},
	}
	for _, tt := range tests {
		if got := shouldPreferPhysicalInterface(tt.addr); got != tt.want {
			t.Fatalf("shouldPreferPhysicalInterface(%q)=%v, want %v", tt.addr, got, tt.want)
		}
	}
}

func TestSelectPhysicalIPv4InterfaceIndexLiveHost(t *testing.T) {
	idx, ok := selectPhysicalIPv4InterfaceIndex()
	if !ok || idx == 0 {
		t.Skip("no active physical IPv4 interface with gateway on this host")
	}
}
