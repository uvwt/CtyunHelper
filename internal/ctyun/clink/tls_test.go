package clink

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"testing"
	"time"
)

func TestVerifyClinkPeerAcceptsExpiredCtYunCertificateForIPEndpoint(t *testing.T) {
	root, rootKey, roots := testRootCA(t)
	leaf := testLeaf(t, root, rootKey, testLeafOptions{
		DNSNames:  []string{"*.ctyun.cn"},
		NotBefore: time.Date(2022, 10, 14, 0, 0, 0, 0, time.UTC),
		NotAfter:  time.Date(2023, 11, 14, 0, 0, 0, 0, time.UTC),
	})

	err := verifyClinkPeer([]*x509.Certificate{leaf}, "180.153.162.5", time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC), roots)
	if err != nil {
		t.Fatalf("expired CtYun certificate should pass scoped compatibility verification: %v", err)
	}
}

func TestCtYunVerificationNameUsesSANWithoutTrustingCommonName(t *testing.T) {
	certificate := &x509.Certificate{DNSNames: []string{"*.edge.ctyun.cn"}, Subject: pkix.Name{CommonName: "example.com"}}
	name, ok := ctyunVerificationName(certificate)
	if !ok || name != "clink.edge.ctyun.cn" {
		t.Fatalf("verification name = %q ok=%v", name, ok)
	}

	unrelated := &x509.Certificate{DNSNames: []string{"*.example.com"}, Subject: pkix.Name{CommonName: "*.ctyun.cn"}}
	if _, ok := ctyunVerificationName(unrelated); ok {
		t.Fatal("common name alone must not enable compatibility verification")
	}
}

func TestVerifyClinkPeerPrefersStandardValidIPEndpointCertificate(t *testing.T) {
	root, rootKey, roots := testRootCA(t)
	now := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	leaf := testLeaf(t, root, rootKey, testLeafOptions{
		IPAddresses: []net.IP{net.ParseIP("180.153.162.5")},
		NotBefore:   now.Add(-time.Hour),
		NotAfter:    now.Add(time.Hour),
	})

	if err := verifyClinkPeer([]*x509.Certificate{leaf}, "180.153.162.5", now, roots); err != nil {
		t.Fatalf("standard valid endpoint certificate should pass: %v", err)
	}
}

func TestVerifyClinkPeerRejectsExpiredWrongDomain(t *testing.T) {
	root, rootKey, roots := testRootCA(t)
	leaf := testLeaf(t, root, rootKey, testLeafOptions{
		DNSNames:  []string{"example.com"},
		NotBefore: time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:  time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
	})

	if err := verifyClinkPeer([]*x509.Certificate{leaf}, "180.153.162.5", time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC), roots); err == nil {
		t.Fatal("expired certificate for unrelated domain must be rejected")
	}
}

func TestVerifyClinkPeerRejectsNotYetValidCtYunCertificate(t *testing.T) {
	root, rootKey, roots := testRootCA(t)
	now := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	leaf := testLeaf(t, root, rootKey, testLeafOptions{
		DNSNames:  []string{"ctyun.cn"},
		NotBefore: now.Add(time.Hour),
		NotAfter:  now.Add(24 * time.Hour),
	})

	if err := verifyClinkPeer([]*x509.Certificate{leaf}, "180.153.162.5", now, roots); err == nil {
		t.Fatal("not-yet-valid certificate must be rejected")
	}
}

type testLeafOptions struct {
	DNSNames    []string
	IPAddresses []net.IP
	NotBefore   time.Time
	NotAfter    time.Time
}

func testRootCA(t *testing.T) (*x509.Certificate, *rsa.PrivateKey, *x509.CertPool) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "CtyunHelper Test Root"},
		NotBefore:             time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:              time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(raw)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	return certificate, key, roots
}

func testLeaf(t *testing.T, root *x509.Certificate, rootKey *rsa.PrivateKey, options testLeafOptions) *x509.Certificate {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "test leaf"},
		DNSNames:     options.DNSNames,
		IPAddresses:  options.IPAddresses,
		NotBefore:    options.NotBefore,
		NotAfter:     options.NotAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	raw, err := x509.CreateCertificate(rand.Reader, template, root, &key.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(raw)
	if err != nil {
		t.Fatal(err)
	}
	return certificate
}
