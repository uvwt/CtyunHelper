package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// SHA256Hex 是通用摘要工具，被请求签名（PublicSignature/ServerNodeSignature）
// 与天翼登录协议同时使用。它不承担任何本地口令存储职责：登录流程只把结果作为
// 一次性传输摘要提交给服务端，算法由天翼服务端固定要求，客户端无权更换。
func SHA256Hex(value string) string {
	digest := sha256.Sum256([]byte(value)) // codeql[go/weak-sensitive-data-hashing] -- 协议规定的传输摘要，非本地口令存储
	return hex.EncodeToString(digest[:])
}

// LoginPassword 复现天翼官方客户端的口令摘要：sha256(sha256(password) + challengeCode)。
// 服务端按该固定算法校验，换成任何更“强”的算法都会导致登录失败。
func LoginPassword(password, challengeCode string) string {
	return SHA256Hex(SHA256Hex(password) + challengeCode)
}

func PublicSignature(ctx RequestContext, profile Profile) string {
	return PublicSignatureWithIdentity(WindowsIdentity(), ctx, profile)
}

func PublicSignatureWithIdentity(identity ClientIdentity, ctx RequestContext, profile Profile) string {
	identity = identity.withDefaults()
	source := identity.DeviceType + ctx.RequestID + itoa(profile.TenantID) + ctx.Timestamp +
		itoa(profile.UserID) + identity.Version + profile.SecretKey
	return strings.ToUpper(SHA256Hex(source))
}

func ServerNodeSignature(ctx RequestContext, profile Profile, serverNode string) string {
	return ServerNodeSignatureWithIdentity(WindowsIdentity(), ctx, profile, serverNode)
}

func ServerNodeSignatureWithIdentity(identity ClientIdentity, ctx RequestContext, profile Profile, serverNode string) string {
	identity = identity.withDefaults()
	path := normalizePath(ctx.Path)
	userIdentity := profile.UserEID
	if userIdentity == "" {
		userIdentity = itoa(profile.UserID)
	}
	source := identity.DeviceType + ctx.RequestID + ctx.Timestamp + userIdentity +
		identity.Version + serverNode + path + profile.SecretKey
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
