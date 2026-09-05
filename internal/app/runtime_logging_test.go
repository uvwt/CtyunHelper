package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/uvwt/CtyunHelper/internal/logging"
)

func TestHealthyBackoffIsLoggedAsInfo(t *testing.T) {
	logger, err := logging.New(logging.Options{Path: filepath.Join(t.TempDir(), "app.log")})
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()

	runtime := NewRuntime(nil, nil, nil, nil, RuntimeOptions{Logger: logger})
	runtime.logStateTransition(State{Connection: ConnectionOnline}, State{Connection: ConnectionBackoff})
	entries := logger.Snapshot(10)
	if len(entries) != 1 || entries[0].Level != logging.LevelInfo {
		t.Fatalf("backoff log = %#v", entries)
	}
}

func TestRuntimePublishesLogEventsAndRecordsStateTransitions(t *testing.T) {
	logger, err := logging.New(logging.Options{Path: filepath.Join(t.TempDir(), "app.log"), MemoryEntries: 50})
	if err != nil {
		t.Fatal(err)
	}
	model := NewModel(State{Connection: ConnectionAuth})
	runtime := NewRuntime(model, nil, nil, nil, RuntimeOptions{Logger: logger})
	events, unsubscribe := model.Events().Subscribe(32)
	defer unsubscribe()

	runtime.Start(context.Background())
	model.Update(func(state *State) {
		state.Connection = ConnectionOnline
		state.DesktopName = "测试云电脑"
		state.Points = 680
	})
	model.Update(func(state *State) {
		state.LastError = "request failed token=should-not-leak"
	})

	deadline := time.After(2 * time.Second)
	logEvents := 0
	// Runtime 启动 1 条；第一次状态更新产生连接/桌面/积分 3 条；
	// 第二次错误状态再产生 1 条。等到第 5 条后再停止，避免测试自己抢先关闭 logger。
	for logEvents < 5 {
		select {
		case event := <-events:
			if event.Type == EventLogAdded {
				logEvents++
			}
		case <-deadline:
			t.Fatalf("only received %d log events", logEvents)
		}
	}
	runtime.Stop()

	entries := logger.Snapshot(50)
	joined := make([]string, 0, len(entries))
	for _, entry := range entries {
		joined = append(joined, entry.Line())
	}
	text := strings.Join(joined, "\n")
	for _, expected := range []string{"Runtime 启动", "连接状态变化", "已选择云电脑", "积分余额更新"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %q in logs: %s", expected, text)
		}
	}
	if strings.Contains(text, "should-not-leak") || !strings.Contains(text, "token=***") {
		t.Fatalf("runtime log redaction failed: %s", text)
	}
}
