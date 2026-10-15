//go:build windows

package clink

import (
	"net/http"
	"net/url"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	winhttpDLL                     = syscall.NewLazyDLL("winhttp.dll")
	getIEProxyConfigForCurrentUser = winhttpDLL.NewProc("WinHttpGetIEProxyConfigForCurrentUser")
	proxyKernel32                  = syscall.NewLazyDLL("kernel32.dll")
	globalFree                     = proxyKernel32.NewProc("GlobalFree")
)

type ieProxyConfig struct {
	AutoDetect    int32
	AutoConfigURL *uint16
	Proxy         *uint16
	ProxyBypass   *uint16
}

func clinkProxy(req *http.Request) (*url.URL, error) {
	proxyURL, err := environmentProxy(req)
	if err != nil || proxyURL != nil {
		return proxyURL, err
	}

	proxy, ok := currentUserProxyConfig()
	if !ok || proxy == "" {
		return nil, nil
	}
	// Clink 是专用中继通道。天翼返回的中继地址可能命中浏览器的私网 bypass
	// 规则，但旧 CtYun/Windows ClientWebSocket 的真实工作链仍经当前用户代理
	// 建立该隧道；这里跟随显式代理本身，不套用面向普通网页的 ProxyOverride。
	return parseProxyServer(proxy, req.URL.Scheme)
}

func currentUserProxyConfig() (proxy string, ok bool) {
	var config ieProxyConfig
	result, _, _ := getIEProxyConfigForCurrentUser.Call(uintptr(unsafe.Pointer(&config)))
	if result == 0 {
		return "", false
	}
	defer freeProxyString(config.AutoConfigURL)
	defer freeProxyString(config.Proxy)
	defer freeProxyString(config.ProxyBypass)
	return utf16ProxyString(config.Proxy), true
}

func freeProxyString(value *uint16) {
	if value != nil {
		globalFree.Call(uintptr(unsafe.Pointer(value)))
	}
}

func utf16ProxyString(value *uint16) string {
	// 统一使用 x/sys 提供的 Windows UTF-16 转换，避免在业务代码里重复维护
	// unsafe 指针遍历；调用方提供的 WinHTTP 字符串本身应以 NUL 结尾。
	return windows.UTF16PtrToString(value)
}
