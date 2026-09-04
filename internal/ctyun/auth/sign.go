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
