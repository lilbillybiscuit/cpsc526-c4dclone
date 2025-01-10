package src

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// HTTP client with timeout
func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
	}
}

// Retry mechanism
func retry(attempts int, sleep time.Duration, f func() error) error {
	var err error
	for i := 0; i < attempts; i++ {
		if err = f(); err == nil {
			return nil
		}
		if i < attempts-1 {
			time.Sleep(sleep * time.Duration(i+1))
		}
	}
	return fmt.Errorf("failed after %d attempts: %v", attempts, err)
}

// JSON helpers
func toJSON(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

func fromJSON(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

// Time helpers
func timeoutContext(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// Logging helpers
type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
)

func (l LogLevel) String() string {
	switch l {
	case LogLevelDebug:
		return "DEBUG"
	case LogLevelInfo:
		return "INFO"
	case LogLevelWarn:
		return "WARN"
	case LogLevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

type Logger struct {
	level LogLevel
}

func (l *Logger) log(level LogLevel, format string, args ...interface{}) {
	if level >= l.level {
		msg := fmt.Sprintf(format, args...)
		fmt.Printf("[%s] %s: %s\n", time.Now().Format(time.RFC3339), level, msg)
	}
}
