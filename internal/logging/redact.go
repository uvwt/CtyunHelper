package logging

import "strings"

var sensitiveKeys = []string{
	"password", "secretkey", "commonloginreqheader", "ticket", "sessionkey", "smscode",
}

func RedactKeyValue(key, value string) string {
	lower := strings.ToLower(key)
	for _, sensitive := range sensitiveKeys {
		if strings.Contains(lower, sensitive) {
			return "***"
		}
	}
	return value
}
