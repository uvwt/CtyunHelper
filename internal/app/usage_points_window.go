package app

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultUsagePointsWindowStart = "04:00"
	defaultUsagePointsWindowEnd   = "07:00"
)

type UsagePointsWindow struct {
	Enabled bool
	Start   string
	End     string
}

type compiledUsagePointsWindow struct {
	UsagePointsWindow
	startMinute int
	endMinute   int
}

type PointsSessionPolicy struct {
	mu      sync.RWMutex
	window  compiledUsagePointsWindow
	changes chan struct{}
}

func NewPointsSessionPolicy(initial UsagePointsWindow) (*PointsSessionPolicy, error) {
	compiled, err := compileUsagePointsWindow(initial)
	if err != nil {
		return nil, err
	}
	return &PointsSessionPolicy{window: compiled, changes: make(chan struct{}, 1)}, nil
}

func (p *PointsSessionPolicy) Snapshot() UsagePointsWindow {
	if p == nil {
		return UsagePointsWindow{Start: defaultUsagePointsWindowStart, End: defaultUsagePointsWindowEnd}
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.window.UsagePointsWindow
}

func (p *PointsSessionPolicy) Update(window UsagePointsWindow) error {
	if p == nil {
		return nil
	}
	compiled, err := compileUsagePointsWindow(window)
	if err != nil {
		return err
	}
	p.mu.Lock()
	changed := p.window != compiled
	p.window = compiled
	p.mu.Unlock()
	if changed {
		select {
		case p.changes <- struct{}{}:
		default:
		}
	}
	return nil
}

func (p *PointsSessionPolicy) Changes() <-chan struct{} {
	if p == nil {
		return nil
	}
	return p.changes
}

func (p *PointsSessionPolicy) FormalAt(now time.Time) bool {
	if p == nil {
		return false
	}
	p.mu.RLock()
	window := p.window
	p.mu.RUnlock()
	if !window.Enabled {
		return false
	}
	minute := now.Hour()*60 + now.Minute()
	if window.startMinute < window.endMinute {
		return minute >= window.startMinute && minute < window.endMinute
	}
	return minute >= window.startMinute || minute < window.endMinute
}

func (p *PointsSessionPolicy) NextBoundary(now time.Time) time.Time {
	if p == nil {
		return time.Time{}
	}
	p.mu.RLock()
	window := p.window
	p.mu.RUnlock()
	if !window.Enabled {
		return time.Time{}
	}
	target := window.startMinute
	if p.FormalAt(now) {
		target = window.endMinute
	}
	return nextMinuteOccurrence(now, target)
}

func normalizeUsagePointsWindow(window UsagePointsWindow) (UsagePointsWindow, error) {
	window.Start = strings.TrimSpace(window.Start)
	window.End = strings.TrimSpace(window.End)
	if window.Start == "" {
		window.Start = defaultUsagePointsWindowStart
	}
	if window.End == "" {
		window.End = defaultUsagePointsWindowEnd
	}
	start, err := parseClockMinute(window.Start)
	if err != nil {
		return UsagePointsWindow{}, fmt.Errorf("刷积分开始时间: %w", err)
	}
	end, err := parseClockMinute(window.End)
	if err != nil {
		return UsagePointsWindow{}, fmt.Errorf("刷积分结束时间: %w", err)
	}
	if window.Enabled && start == end {
		return UsagePointsWindow{}, fmt.Errorf("刷积分开始和结束时间不能相同")
	}
	window.Start = formatClockMinute(start)
	window.End = formatClockMinute(end)
	return window, nil
}

func compileUsagePointsWindow(window UsagePointsWindow) (compiledUsagePointsWindow, error) {
	normalized, err := normalizeUsagePointsWindow(window)
	if err != nil {
		return compiledUsagePointsWindow{}, err
	}
	start, _ := parseClockMinute(normalized.Start)
	end, _ := parseClockMinute(normalized.End)
	return compiledUsagePointsWindow{UsagePointsWindow: normalized, startMinute: start, endMinute: end}, nil
}

func parseClockMinute(value string) (int, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 || len(parts[0]) != 2 || len(parts[1]) != 2 {
		return 0, fmt.Errorf("时间格式应为 HH:MM")
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, fmt.Errorf("小时必须在 00-23")
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, fmt.Errorf("分钟必须在 00-59")
	}
	return hour*60 + minute, nil
}

func formatClockMinute(value int) string {
	return fmt.Sprintf("%02d:%02d", value/60, value%60)
}

func nextMinuteOccurrence(now time.Time, minute int) time.Time {
	candidate := time.Date(now.Year(), now.Month(), now.Day(), minute/60, minute%60, 0, 0, now.Location())
	if !candidate.After(now) {
		candidate = candidate.AddDate(0, 0, 1)
	}
	return candidate
}
