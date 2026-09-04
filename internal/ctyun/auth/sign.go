package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func SHA256Hex(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func LoginPassword(password, challengeCode string) string {
	return SHA256Hex(SHA256Hex(password) + challengeCode)
}

func PublicSignature(ctx RequestContext, profile Profile) string {
	source := NativeDeviceType + ctx.RequestID + itoa(profile.TenantID) + ctx.Timestamp +
		itoa(profile.UserID) + NativeVersion + profile.SecretKey
	return strings.ToUpper(SHA256Hex(source))
}

func ServerNodeSignature(ctx RequestContext, profile Profile, serverNode string) string {
	path := normalizePath(ctx.Path)
	source := NativeDeviceType + ctx.RequestID + ctx.Timestamp + profile.UserEID +
		NativeVersion + serverNode + path + profile.SecretKey
	return strings.ToUpper(SHA256Hex(source))
}

func normalizePath(path string) string {
	path = "/" + strings.TrimLeft(path, "/")
	if index := strings.IndexAny(path, "?#"); index >= 0 {
		path = path[:index]
	}
	return path
}

func itoa(value int64) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var buf [20]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
