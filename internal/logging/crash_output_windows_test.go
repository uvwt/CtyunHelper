//go:build windows

package logging

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const crashOutputTestPathEnv = "CTYUNHELPER_CRASH_OUTPUT_TEST_PATH"

func TestRedirectCrashOutputCapturesRuntimePanic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crash.log")
	cmd := exec.Command(os.Args[0], "-test.run=TestRedirectCrashOutputChild")
	cmd.Env = append(os.Environ(), crashOutputTestPathEnv+"="+path)
	if err := cmd.Run(); err == nil {
		t.Fatal("panic child unexpectedly exited successfully")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if !strings.Contains(text, "panic: crash-output-test") || !strings.Contains(text, "TestRedirectCrashOutputChild") {
		t.Fatalf("runtime panic stack missing from crash output:\n%s", text)
	}
}

func TestRedirectCrashOutputChild(t *testing.T) {
	path := os.Getenv(crashOutputTestPathEnv)
	if path == "" {
		return
	}
	output, err := RedirectCrashOutput(path)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	go func() {
		panic("crash-output-test")
	}()
	select {}
}
