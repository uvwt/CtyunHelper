package storage

import "testing"

func TestStartupCommandAlwaysQuotesExecutable(t *testing.T) {
	startup, err := NewStartup(`C:\Program Files\CtyunHelper\CtyunHelper.exe`)
	if err != nil {
		t.Fatal(err)
	}
	if got := startup.Command(); got != `"C:\Program Files\CtyunHelper\CtyunHelper.exe"` {
		t.Fatalf("command = %q", got)
	}
}

func TestStartupRejectsAmbiguousExecutable(t *testing.T) {
	for _, value := range []string{"", `C:\bad"name.exe`} {
		if _, err := NewStartup(value); err == nil {
			t.Fatalf("expected invalid executable %q", value)
		}
	}
}
