package eai

import (
	"strings"
	"testing"
)

func TestDecryptAESECBBase64IgnoresTransportWhitespace(t *testing.T) {
	key := []byte("0123456789abcdef")
	plain := []byte("session-key-for-tests")
	encoded := encryptAESECBBase64ForTest(t, plain, key)
	if len(encoded) < 8 {
		t.Fatalf("encoded test fixture too short: %d", len(encoded))
	}

	// 线上 ticketAuthorize 已观察到 Base64 密文中间插入空格；同时覆盖常见
	// CR/LF、Tab，确保这里只忽略传输空白，不改变密文字符本身。
	wrapped := encoded[:8] + " \r\n\t" + encoded[8:]
	got, err := decryptAESECBBase64(wrapped, key)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(plain) {
		t.Fatalf("decrypted = %q, want %q", got, plain)
	}
}

func TestDecryptAESECBBase64StillRejectsInvalidCharacters(t *testing.T) {
	key := []byte("0123456789abcdef")
	encoded := encryptAESECBBase64ForTest(t, []byte("session-key-for-tests"), key)
	_, err := decryptAESECBBase64(encoded[:8]+"!"+encoded[8:], key)
	if err == nil || !strings.Contains(err.Error(), "base64 解码失败") {
		t.Fatalf("error = %v", err)
	}
}
