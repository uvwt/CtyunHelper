package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoggerRedactsFieldsAndMessageBeforeDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	fixed := time.Date(2026, 9, 4, 19, 0, 0, 0, time.Local)
	logger, err := New(Options{Path: path, Now: func() time.Time { return fixed }})
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("auth", "login failed secretKey=server-secret password:plain Authorization: Bearer bearer-secret", String("token", "abc"), String("status", "failed"))
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{"server-secret", "plain", "token=abc", "bearer-secret"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("secret %q leaked into log: %s", forbidden, text)
		}
	}
	if !strings.Contains(text, "secretKey=***") || !strings.Contains(text, "password=***") || !strings.Contains(text, "Authorization=***") || !strings.Contains(text, "token=***") || !strings.Contains(text, "status=failed") {
		t.Fatalf("redacted log = %q", text)
	}
}

func TestLoggerRotatesAndBoundsBackups(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "app.log")
	logger, err := New(Options{Path: path, MaxBytes: 120, Backups: 2, MemoryEntries: 10})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 50; index++ {
		logger.Info("test", strings.Repeat("x", 40), Int("index", index))
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{path, path + ".1", path + ".2"} {
		if info, err := os.Stat(file); err != nil || info.Size() == 0 {
			t.Fatalf("expected non-empty %s, info=%v err=%v", file, info, err)
		}
	}
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Fatalf("unexpected backup beyond limit: %v", err)
	}
	if got := len(logger.Snapshot(0)); got != 10 {
		t.Fatalf("memory entries = %d, want 10", got)
	}
}

func TestLoggerSnapshotIsDefensiveCopyAndCallbackOutsideLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	logger, err := New(Options{Path: path, MemoryEntries: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	callbackCalls := 0
	logger.SetOnEntry(func(entry Entry) {
		callbackCalls++
		// Snapshot 会再次取得读锁；若 callback 在写锁内执行，这里会死锁。
		_ = logger.Snapshot(1)
	})
	logger.Info("app", "started", String("status", "ok"))
	if callbackCalls != 1 {
		t.Fatalf("callback calls = %d", callbackCalls)
	}
	snapshot := logger.Snapshot(1)
	snapshot[0].Message = "changed"
	snapshot[0].Fields[0].Value = "changed"
	original := logger.Snapshot(1)[0]
	if original.Message != "started" || original.Fields[0].Value != "ok" {
		t.Fatalf("snapshot mutated logger state: %#v", original)
	}
}
