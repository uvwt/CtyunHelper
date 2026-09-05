package clink

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"strings"
	"time"
)

func newClinkTLSConfig(endpoint string) *tls.Config {
	host := clinkEndpointHost(endpoint)
	return &tls.Config{
		// Clink 目前以 IP 作为 wss endpoint，但服务端返回的是 *.ctyun.cn 证书，
		// 且线上仍存在已经过期的旧证书。关闭 Go 的默认 verifier 后立即由
		// VerifyConnection 执行更严格的 CtYun 专用验证，不做无条件放行。
		InsecureSkipVerify: true, //nolint:gosec -- custom verification below is mandatory
		VerifyConnection: func(state tls.ConnectionState) error {
			return verifyClinkPeer(state.PeerCertificates, host, time.Now(), nil)
		},
	}
}

func clinkEndpointHost(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if host, _, err := net.SplitHostPort(endpoint); err == nil {
		return host
	}
	return strings.Trim(endpoint, "[]")
}

func verifyClinkPeer(certificates []*x509.Certificate, endpointHost string, now time.Time, roots *x509.CertPool) error {
	if len(certificates) == 0 {
		return fmt.Errorf("clink: TLS 服务端未提供证书")
	}
	leaf := certificates[0]
	intermediates := x509.NewCertPool()
	for _, certificate := range certificates[1:] {
		intermediates.AddCert(certificate)
	}

	// 新节点如果已经提供与 endpoint 匹配且当前有效的证书，优先走标准验证。
	if _, err := leaf.Verify(x509.VerifyOptions{
		DNSName:       endpointHost,
		Roots:         roots,
		Intermediates: intermediates,
		CurrentTime:   now,
	}); err == nil {
		return nil
	}

	// 兼容路径只处理当前线上已观察到的“IP endpoint + 已过期 ctyun.cn 证书”。
	// 未来证书尚未生效时仍拒绝，避免把机器时间错误或异常证书当成兼容情况。
	if net.ParseIP(endpointHost) == nil {
		return fmt.Errorf("clink: TLS 证书与目标 %s 不匹配", endpointHost)
	}
	if !now.After(leaf.NotAfter) {
		return fmt.Errorf("clink: TLS 证书未通过当前有效期校验")
	}
	verificationName, ok := ctyunVerificationName(leaf)
	if !ok {
		return fmt.Errorf("clink: TLS 证书 SAN 不属于 ctyun.cn")
	}

	// 把验证时间放回叶子证书原有效期中点，只忽略“现在已经过期”这一项；
	// 域名、签名、根证书和中间证书链仍由 x509.Verify 完整校验。
	validAt := leaf.NotBefore.Add(leaf.NotAfter.Sub(leaf.NotBefore) / 2)
	if _, err := leaf.Verify(x509.VerifyOptions{
		DNSName:       verificationName,
		Roots:         roots,
		Intermediates: intermediates,
		CurrentTime:   validAt,
	}); err != nil {
		return fmt.Errorf("clink: TLS 兼容校验失败: %w", err)
	}
	return nil
}
func ctyunVerificationName(certificate *x509.Certificate) (string, bool) {
	for _, raw := range certificate.DNSNames {
		name := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "."))
		if name == "ctyun.cn" {
			return name, true
		}
		if strings.HasPrefix(name, "*.") {
			suffix := strings.TrimPrefix(name, "*.")
			if suffix == "ctyun.cn" || strings.HasSuffix(suffix, ".ctyun.cn") {
				return "clink." + suffix, true
			}
			continue
		}
		if strings.HasSuffix(name, ".ctyun.cn") {
			return name, true
		}
	}
	return "", false
}
