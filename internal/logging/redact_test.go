package logging

import "testing"

func TestRedactSensitiveValues(t *testing.T) {
	if got := RedactKeyValue("secretKey", "abc"); got != "***" {
		t.Fatalf("got %q", got)
	}
	if got := RedactKeyValue("status", "online"); got != "online" {
		t.Fatalf("got %q", got)
	}
}
