package src

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

const threshold = 1 * time.Second

type LockInfo struct {
	time     time.Time
	file     string
	line     int
	funcName string
}

type MonitoredRWMutex struct {
	mu                 sync.RWMutex
	lockInfo           atomic.Pointer[LockInfo]
	threshold          time.Duration
	shouldNotifyUnlock atomic.Bool
	done               chan struct{} // Channel to signal monitor goroutine to stop
}

//func NewMonitoredRWMutex(threshold time.Duration) *MonitoredRWMutex {
//	return &MonitoredRWMutex{
//		threshold: threshold,
//		done:      make(chan struct{}),
//	}
//}

func getCaller(skip int) (string, int, string) {
	pc, file, line, ok := runtime.Caller(skip)
	if !ok {
		return "unknown", 0, "unknown"
	}

	fn := runtime.FuncForPC(pc)
	funcName := "unknown"
	if fn != nil {
		funcName = fn.Name()
	}

	return file, line, funcName
}

func (m *MonitoredRWMutex) Lock() {
	m.mu.Lock()
	m.shouldNotifyUnlock.Store(false)

	// Signal any existing monitor to stop
	select {
	case m.done <- struct{}{}:
	default:
	}

	file, line, funcName := getCaller(2)
	info := &LockInfo{
		time:     time.Now(),
		file:     file,
		line:     line,
		funcName: funcName,
	}
	m.lockInfo.Store(info)

	// Create new done channel for new monitor
	m.done = make(chan struct{})
	go m.monitor()
}

func (m *MonitoredRWMutex) Unlock() {
	// Signal monitor to stop before unlocking
	select {
	case m.done <- struct{}{}:
	default:
	}

	m.lockInfo.Store(nil)
	if m.shouldNotifyUnlock.Load() {
		fmt.Println("Unlocking")
	}
	m.mu.Unlock()
}

func (m *MonitoredRWMutex) RLock() {
	m.mu.RLock()
	m.shouldNotifyUnlock.Store(false)

	// Signal any existing monitor to stop
	select {
	case m.done <- struct{}{}:
	default:
	}

	file, line, funcName := getCaller(2)
	info := &LockInfo{
		time:     time.Now(),
		file:     file,
		line:     line,
		funcName: funcName,
	}
	m.lockInfo.Store(info)

	// Create new done channel for new monitor
	m.done = make(chan struct{})
	go m.monitor()
}

func (m *MonitoredRWMutex) RUnlock() {
	// Signal monitor to stop before unlocking
	select {
	case m.done <- struct{}{}:
	default:
	}

	m.lockInfo.Store(nil)
	if m.shouldNotifyUnlock.Load() {
		fmt.Println("RUnlocking")
	}
	m.mu.RUnlock()
}

func (m *MonitoredRWMutex) monitor() {
	ticker := time.NewTicker(threshold / 2)
	defer ticker.Stop()

	for {
		select {
		case <-m.done:
			return
		case <-ticker.C:
			info := m.lockInfo.Load()
			if info == nil {
				return
			}

			if time.Since(info.time) > threshold {
				m.shouldNotifyUnlock.Store(true)
				fmt.Printf("Warning: Lock held for more than %v\n", threshold)
				fmt.Printf("Lock acquired at %s:%d in function %s\n",
					info.file,
					info.line,
					info.funcName)
				return
			}
		}
	}
}

func (m *MonitoredRWMutex) GetLockInfo() string {
	info := m.lockInfo.Load()
	if info == nil {
		return "Lock not held"
	}

	duration := time.Since(info.time)
	return fmt.Sprintf("Lock held for %v at %s:%d in %s",
		duration,
		info.file,
		info.line,
		info.funcName)
}
