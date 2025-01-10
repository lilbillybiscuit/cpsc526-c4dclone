package src

import (
	"sync"
	"time"
)

type MetricsCollector struct {
	buffer     []Metric
	bufferSize int
	mu         sync.RWMutex
}

type Metric struct {
	Timestamp time.Time
	NodeID    string
	Type      string
	Value     float64
}

func NewMetricsCollector(bufferSize int) *MetricsCollector {
	return &MetricsCollector{
		buffer:     make([]Metric, 0, bufferSize),
		bufferSize: bufferSize,
	}
}

func (mc *MetricsCollector) Add(metric Metric) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if len(mc.buffer) >= mc.bufferSize {
		mc.buffer = mc.buffer[1:]
	}
	mc.buffer = append(mc.buffer, metric)
}

func (mc *MetricsCollector) GetMetrics(duration time.Duration) []Metric {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	cutoff := time.Now().Add(-duration)
	metrics := make([]Metric, 0)

	for _, m := range mc.buffer {
		if m.Timestamp.After(cutoff) {
			metrics = append(metrics, m)
		}
	}

	return metrics
}
