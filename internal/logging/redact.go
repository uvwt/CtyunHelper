package logging

import (
	"regexp"
	"strings"
)

var sensitiveKeys = []string{
	"password", "secretkey", "commonloginreqheader", "ticket", "sessionkey", "smscode",
	"verificationcode", "captcha", "authorization", "token", "devicecode", "authdata",
	"clientkey", "iamticket",
}

var sensitiveTextPattern = regexp.MustCompile(`(?i)\b(password|secretkey|commonloginreqheader|ticket|sessionkey|smscode|verificationcode|captcha|authorization|token|devicecode|authdata|clientkey|iamticket)\s*[:=]\s*[^\s,;]+`)
var authorizationBearerPattern = regexp.MustCompile(`(?i)\bauthorization\s*[:=]\s*bearer\s+[^\s,;]+`)

func RedactKeyValue(key, value string) string {
	lower := strings.ToLower(key)
	for _, sensitive := range sensitiveKeys {
		if strings.Contains(lower, sensitive) {
			return "***"
		}
	}
	return value
}

func RedactText(value string) string {
	value = authorizationBearerPattern.ReplaceAllString(value, "Authorization=***")
	return sensitiveTextPattern.ReplaceAllStringFunc(value, func(match string) string {
		separator := strings.IndexAny(match, ":=")
		if separator < 0 {
			return "***"
		}
		return strings.TrimSpace(match[:separator]) + "=***"
	})
}
