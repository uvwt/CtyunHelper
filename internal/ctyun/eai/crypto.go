package eai

import (
	"bytes"
	"crypto/aes"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"strings"
	"unicode"
)

var gatewayAESKey = []byte("chinatelecom@cnn")

func decryptAESECBBase64(value string, key []byte) ([]byte, error) {
	// EAI 偶尔会在 Base64 密文中插入空格或其他空白字符；这类空白只是
	// 传输格式，不属于密文本身。统一剔除 Unicode 空白后再做标准 Base64
	// 解码，AES/PKCS7 校验仍保持严格，不放宽任何密码学验证。
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, value)
	raw, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		return nil, fmt.Errorf("eai: base64 解码失败: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("eai: AES key 无效: %w", err)
	}
	if len(raw) == 0 || len(raw)%block.BlockSize() != 0 {
		return nil, fmt.Errorf("eai: AES-ECB 密文长度无效")
	}
	plain := make([]byte, len(raw))
	for offset := 0; offset < len(raw); offset += block.BlockSize() {
		block.Decrypt(plain[offset:offset+block.BlockSize()], raw[offset:offset+block.BlockSize()])
	}
	return unpadPKCS7(plain, block.BlockSize())
}

func unpadPKCS7(value []byte, blockSize int) ([]byte, error) {
	if len(value) == 0 || len(value)%blockSize != 0 {
		return nil, fmt.Errorf("eai: PKCS7 数据长度无效")
	}
	padding := int(value[len(value)-1])
	if padding == 0 || padding > blockSize || padding > len(value) {
		return nil, fmt.Errorf("eai: PKCS7 padding 无效")
	}
	if !bytes.Equal(value[len(value)-padding:], bytes.Repeat([]byte{byte(padding)}, padding)) {
		return nil, fmt.Errorf("eai: PKCS7 padding 内容无效")
	}
	return value[:len(value)-padding], nil
}

func parseRSAPublicKey(value string) (*rsa.PublicKey, error) {
	cleaned := strings.TrimSpace(value)
	if block, _ := pem.Decode([]byte(cleaned)); block != nil {
		cleaned = base64.StdEncoding.EncodeToString(block.Bytes)
	}
	cleaned = strings.NewReplacer("\r", "", "\n", "", " ", "", "\t", "").Replace(cleaned)
	der, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		return nil, fmt.Errorf("eai: SSO 公钥 base64 无效: %w", err)
	}
	if parsed, err := x509.ParsePKIXPublicKey(der); err == nil {
		if key, ok := parsed.(*rsa.PublicKey); ok {
			return key, nil
		}
		return nil, fmt.Errorf("eai: SSO 公钥不是 RSA")
	}
	if key, err := x509.ParsePKCS1PublicKey(der); err == nil {
		return key, nil
	}
	return nil, fmt.Errorf("eai: 无法解析 SSO RSA 公钥")
}
