package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Level string

const (
	LevelDebug Level = "DEBUG"
	LevelInfo  Level = "INFO"
	LevelWarn  Level = "WARN"
	LevelError Level = "ERROR"
)

type Field struct {
	Key   string
	Value string
}

func String(key, value string) Field {
	return Field{Key: key, Value: value}
}

func Int(key string, value int) Field {
	return Field{Key: key, Value: fmt.Sprintf("%d", value)}
}

type Entry struct {
	Time      time.Time
	Level     Level
	Component string
	Message   string
	Fields    []Field
}

func (e Entry) Line() string {
	var builder strings.Builder
	builder.WriteString(e.Time.Format("2006-01-02 15:04:05.000"))
	builder.WriteByte(' ')
	builder.WriteString(string(e.Level))
	builder.WriteByte(' ')
	builder.WriteString(e.Component)
	builder.WriteByte(' ')
	builder.WriteString(e.Message)
	for _, field := range e.Fields {
		builder.WriteByte(' ')
		builder.WriteString(field.Key)
		builder.WriteByte('=')
		builder.WriteString(field.Value)
	}
	return builder.String()
}

type Options struct {
	Path          string
	MaxBytes      int64
	Backups       int
	MemoryEntries int
	Now           func() time.Time
}

type Logger struct {
	mu sync.RWMutex

	path          string
	maxBytes      int64
	backups       int
	memoryEntries int
	now           func() time.Time
	file          *os.File
	size          int64
	entries       []Entry
	lastError     error
	closed        bool
	onEntry       func(Entry)
}

func New(options Options) (*Logger, error) {
	if strings.TrimSpace(options.Path) == "" {
		return nil, fmt.Errorf("logging: 日志路径不能为空")
	}
	if options.MaxBytes <= 0 {
		options.MaxBytes = 5 << 20
	}
	if options.Backups <= 0 {
		options.Backups = 4
	}
	if options.MemoryEntries <= 0 {
		options.MemoryEntries = 1000
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if err := os.MkdirAll(filepath.Dir(options.Path), 0o700); err != nil {
		return nil, fmt.Errorf("logging: 创建日志目录: %w", err)
	}
	file, err := os.OpenFile(options.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("logging: 打开日志文件: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("logging: 读取日志文件状态: %w", err)
	}
	return &Logger{
		path: options.Path, maxBytes: options.MaxBytes, backups: options.Backups,
		memoryEntries: options.MemoryEntries, now: options.Now, file: file, size: info.Size(),
	}, nil
}

// SetOnEntry 设置内存/文件写入完成后的轻量通知。回调在 Logger 锁外执行，
// 不允许依赖日志写入成功来保证业务正确性；它只用于 UI 事件通知。
func (l *Logger) SetOnEntry(callback func(Entry)) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.onEntry = callback
	l.mu.Unlock()
}

func (l *Logger) Debug(component, message string, fields ...Field) {
	l.log(LevelDebug, component, message, fields...)
}
func (l *Logger) Info(component, message string, fields ...Field) {
	l.log(LevelInfo, component, message, fields...)
}
func (l *Logger) Warn(component, message string, fields ...Field) {
	l.log(LevelWarn, component, message, fields...)
}
func (l *Logger) Error(component, message string, fields ...Field) {
	l.log(LevelError, component, message, fields...)
}

func (l *Logger) Snapshot(limit int) []Entry {
	if l == nil {
		return nil
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	start := 0
	if limit > 0 && len(l.entries) > limit {
		start = len(l.entries) - limit
	}
	result := make([]Entry, len(l.entries)-start)
	for index, entry := range l.entries[start:] {
		result[index] = cloneEntry(entry)
	}
	return result
}

func (l *Logger) LastError() error {
	if l == nil {
		return nil
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.lastError
}

func (l *Logger) Path() string {
	if l == nil {
		return ""
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.path
}

func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	if l.file == nil {
		return nil
	}
	return l.file.Close()
}

func (l *Logger) log(level Level, component, message string, fields ...Field) {
	if l == nil {
		return
	}
	entry := Entry{
		Time: l.now(), Level: level,
		Component: sanitizeToken(component),
		Message:   RedactText(sanitizeText(message)),
		Fields:    sanitizeFields(fields),
	}
	line := entry.Line() + "\n"

	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	l.entries = append(l.entries, entry)
	if len(l.entries) > l.memoryEntries {
		over := len(l.entries) - l.memoryEntries
		copy(l.entries, l.entries[over:])
		l.entries = l.entries[:l.memoryEntries]
	}
	if l.file != nil {
		if err := l.rotateIfNeededLocked(int64(len(line))); err != nil {
			l.lastError = err
		} else if written, err := l.file.WriteString(line); err != nil {
			l.lastError = fmt.Errorf("logging: 写入日志: %w", err)
		} else {
			l.size += int64(written)
		}
	}
	callback := l.onEntry
	l.mu.Unlock()

	if callback != nil {
		callback(cloneEntry(entry))
	}
}

func (l *Logger) rotateIfNeededLocked(incoming int64) (resultErr error) {
	if l.file == nil || l.maxBytes <= 0 || l.size+incoming <= l.maxBytes {
		return nil
	}
	if err := l.file.Close(); err != nil {
		return fmt.Errorf("logging: 关闭待轮转日志: %w", err)
	}
	l.file = nil
	// Windows 的 Rename 不会像 Unix 一样可靠覆盖已存在目标，所以轮转前先
	// 删除最老备份，再从后向前移动，保证每个目标路径都为空。
	if l.backups > 0 {
		oldest := fmt.Sprintf("%s.%d", l.path, l.backups)
		if err := os.Remove(oldest); err != nil && !os.IsNotExist(err) {
			resultErr = fmt.Errorf("logging: 删除最老备份: %w", err)
		}
	}
	if resultErr == nil {
		for index := l.backups - 1; index >= 1; index-- {
			oldPath := fmt.Sprintf("%s.%d", l.path, index)
			newPath := fmt.Sprintf("%s.%d", l.path, index+1)
			if _, err := os.Stat(oldPath); err == nil {
				if err := os.Rename(oldPath, newPath); err != nil {
					resultErr = fmt.Errorf("logging: 轮转备份 %d: %w", index, err)
					break
				}
			} else if !os.IsNotExist(err) {
				resultErr = fmt.Errorf("logging: 检查轮转备份 %d: %w", index, err)
				break
			}
		}
	}
	if resultErr == nil && l.backups > 0 {
		if _, err := os.Stat(l.path); err == nil {
			if err := os.Rename(l.path, l.path+".1"); err != nil {
				resultErr = fmt.Errorf("logging: 轮转当前日志: %w", err)
			}
		} else if !os.IsNotExist(err) {
			resultErr = fmt.Errorf("logging: 检查当前日志: %w", err)
		}
	}
	if resultErr == nil {
		file, err := os.OpenFile(l.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			resultErr = fmt.Errorf("logging: 创建新日志文件: %w", err)
		} else {
			l.file = file
			l.size = 0
			return nil
		}
	}

	// 轮转失败不应该永久关闭日志。尽力重新打开当前路径继续追加；原始轮转
	// 错误保留在 LastError 供 UI/诊断查看。
	if file, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		l.file = file
		if info, statErr := file.Stat(); statErr == nil {
			l.size = info.Size()
		}
	}
	return resultErr
}

func sanitizeFields(fields []Field) []Field {
	if len(fields) == 0 {
		return nil
	}
	result := make([]Field, 0, len(fields))
	for _, field := range fields {
		key := sanitizeToken(field.Key)
		if key == "" {
			continue
		}
		value := RedactKeyValue(key, sanitizeText(field.Value))
		result = append(result, Field{Key: key, Value: value})
	}
	// 让同一条业务日志的字段顺序稳定，便于 diff/排查。
	sort.SliceStable(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result
}

func sanitizeToken(value string) string {
	value = sanitizeText(value)
	return strings.ReplaceAll(value, " ", "_")
}

func sanitizeText(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\t", " ")
	return strings.TrimSpace(value)
}

func cloneEntry(entry Entry) Entry {
	result := entry
	result.Fields = append([]Field(nil), entry.Fields...)
	return result
}
