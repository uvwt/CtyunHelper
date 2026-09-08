package logging

import (
	"fmt"
	"os"
	"runtime/debug"
	"sync"
	"time"
)

var panicOutputMu sync.Mutex

// RecoverPanic 用于 goroutine / UI 回调边界。它只恢复真正的 panic，正常错误仍由业务层显式返回。
// 完整堆栈直接写入 stderr；Windows GUI 入口会把 stderr 重定向到独立 crash 日志。
func RecoverPanic(component string) {
	if recovered := recover(); recovered != nil {
		RecordPanic(component, recovered, debug.Stack())
	}
}

// RecordPanic 记录已由调用方 recover 的 panic。调用方可以在记录后把 panic 转换为业务错误并继续既有重试流程。
func RecordPanic(component string, recovered any, stack []byte) {
	component = sanitizeToken(component)
	if component == "" {
		component = "unknown"
	}
	message := RedactText(fmt.Sprint(recovered))
	stackText := RedactText(string(stack))

	panicOutputMu.Lock()
	defer panicOutputMu.Unlock()
	_, _ = fmt.Fprintf(
		os.Stderr,
		"%s PANIC %s %s\n%s\n",
		time.Now().Format("2006-01-02 15:04:05.000"),
		component,
		message,
		stackText,
	)
}
