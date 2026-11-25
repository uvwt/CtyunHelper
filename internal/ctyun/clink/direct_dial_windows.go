//go:build windows

package clink

import (
	"context"
	"encoding/binary"
	"net"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	ipUnicastIfOption = 31 // IP_UNICAST_IF (IPPROTO_IP)

	ifTypeEthernetCSMACD = 6
	ifTypeIEEE80211      = 71
	ifTypePropVirtual    = 53
	ifTypeTunnel         = 131
	ifTypeLoopback       = 24
)

type adapterCandidate struct {
	ifIndex      uint32
	ifType       uint32
	operStatus   uint32
	friendlyName string
	description  string
	hasGateway   bool
	ipv4Metric   uint32
}

// directNetDialContext 在 Windows 上用于 Clink 直连/回退直连拨号。
// 当目标为非回环地址且本机存在具有默认网关的物理网卡（以太网/Wi-Fi）时，
// 优先通过 IP_UNICAST_IF 绑定物理网卡出口，避免流量被系统 TUN 虚拟网卡
// （如 singbox_tun / Wintun strict_route）劫持拖死；若绑定物理网卡拨号失败，
// 再自动回退到系统默认路由拨号以保持多网络环境兼容。
func directNetDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	if !shouldPreferPhysicalInterface(addr) {
		return dialer.DialContext(ctx, network, addr)
	}
	ifIndex, ok := selectPhysicalIPv4InterfaceIndex()
	if !ok {
		return dialer.DialContext(ctx, network, addr)
	}

	boundDialer := &net.Dialer{
		Timeout:   dialer.Timeout,
		KeepAlive: dialer.KeepAlive,
		Control:   bindSocketToIPv4Interface(ifIndex),
	}
	conn, err := boundDialer.DialContext(ctx, network, addr)
	if err == nil {
		return conn, nil
	}
	if ctx.Err() != nil {
		return nil, err
	}
	return dialer.DialContext(ctx, network, addr)
}

func shouldPreferPhysicalInterface(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.TrimSpace(host)
	if host == "" || strings.EqualFold(host, "localhost") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.To4() == nil {
			return false
		}
	}
	return true
}

func bindSocketToIPv4Interface(ifIndex uint32) func(network, address string, c syscall.RawConn) error {
	// Windows IP_UNICAST_IF 对 IPv4 要求网络字节序的接口索引（见 MSDN IPPROTO_IP）。
	var raw [4]byte
	binary.BigEndian.PutUint32(raw[:], ifIndex)
	netOrderIndex := *(*int)(unsafe.Pointer(&raw[0]))

	return func(network, _ string, c syscall.RawConn) error {
		if network != "tcp4" && network != "udp4" && network != "tcp" && network != "udp" {
			return nil
		}
		var setErr error
		ctrlErr := c.Control(func(fd uintptr) {
			setErr = windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IP, ipUnicastIfOption, netOrderIndex)
		})
		if ctrlErr != nil {
			return ctrlErr
		}
		return setErr
	}
}

func selectPhysicalIPv4InterfaceIndex() (uint32, bool) {
	candidates, err := listIPv4Adapters()
	if err != nil {
		return 0, false
	}
	return pickPhysicalIPv4Interface(candidates)
}

func listIPv4Adapters() ([]adapterCandidate, error) {
	var size uint32 = 16384
	for attempt := 0; attempt < 3; attempt++ {
		buf := make([]byte, size)
		first := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0]))
		err := windows.GetAdaptersAddresses(windows.AF_INET, windows.GAA_FLAG_INCLUDE_GATEWAYS, 0, first, &size)
		if err == windows.ERROR_BUFFER_OVERFLOW {
			continue
		}
		if err != nil {
			return nil, err
		}
		var out []adapterCandidate
		for a := first; a != nil; a = a.Next {
			out = append(out, adapterCandidate{
				ifIndex:      a.IfIndex,
				ifType:       a.IfType,
				operStatus:   a.OperStatus,
				friendlyName: windows.UTF16PtrToString(a.FriendlyName),
				description:  windows.UTF16PtrToString(a.Description),
				hasGateway:   a.FirstGatewayAddress != nil,
				ipv4Metric:   a.Ipv4Metric,
			})
		}
		return out, nil
	}
	return nil, windows.ERROR_BUFFER_OVERFLOW
}

func pickPhysicalIPv4Interface(candidates []adapterCandidate) (uint32, bool) {
	var best adapterCandidate
	found := false
	for _, c := range candidates {
		if !isPhysicalIPv4Candidate(c) {
			continue
		}
		if !found || compareAdapterPriority(c, best) < 0 {
			best = c
			found = true
		}
	}
	if !found {
		return 0, false
	}
	return best.ifIndex, true
}

func isPhysicalIPv4Candidate(c adapterCandidate) bool {
	if c.ifIndex == 0 || c.operStatus != windows.IfOperStatusUp || !c.hasGateway {
		return false
	}
	if c.ifType == ifTypeLoopback || c.ifType == ifTypeTunnel || c.ifType == ifTypePropVirtual {
		return false
	}
	if c.ifType != ifTypeEthernetCSMACD && c.ifType != ifTypeIEEE80211 {
		return false
	}
	if isVirtualTunnelAdapterName(c.friendlyName) || isVirtualTunnelAdapterName(c.description) {
		return false
	}
	return true
}

func isVirtualTunnelAdapterName(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	if lower == "" {
		return false
	}
	for _, keyword := range []string{
		"singbox",
		"sing-box",
		"wintun",
		"wireguard",
		"tailscale",
		"clash",
		"mihomo",
		"v2ray",
		"xray",
		"tap-windows",
		"tun",
		"zerotier",
		"loopback",
	} {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}

func compareAdapterPriority(a, b adapterCandidate) int {
	// 以太网优先于 Wi-Fi，其次按 IPv4 Metric 升序，最后按 IfIndex 升序保证确定性。
	aRank := adapterTypeRank(a.ifType)
	bRank := adapterTypeRank(b.ifType)
	if aRank != bRank {
		if aRank < bRank {
			return -1
		}
		return 1
	}
	if a.ipv4Metric != b.ipv4Metric {
		if a.ipv4Metric < b.ipv4Metric {
			return -1
		}
		return 1
	}
	if a.ifIndex < b.ifIndex {
		return -1
	}
	if a.ifIndex > b.ifIndex {
		return 1
	}
	return 0
}

func adapterTypeRank(ifType uint32) int {
	switch ifType {
	case ifTypeEthernetCSMACD:
		return 0
	case ifTypeIEEE80211:
		return 1
	default:
		return 2
	}
}
