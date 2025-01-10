package src

import (
	"c4d-server/proto"
	"sync"
)

// MetricsProcessor handles metrics collection and processing
type MetricsProcessor struct {
	latencyMatrix *LatencyMatrix
	metrics       *ServerMetrics
	mu            sync.RWMutex
}

func NewMetricsProcessor(activeNodes int) *MetricsProcessor {
	return &MetricsProcessor{
		latencyMatrix: NewLatencyMatrix(activeNodes),
		metrics:       &ServerMetrics{},
	}
}

func (mp *MetricsProcessor) ProcessMetrics(nodeID string, metrics *c4d.Metrics) error {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	// Update latency matrix
	if metrics.RankLatencies != nil {
		for remoteRank, latency := range metrics.RankLatencies.AvgLatencies {
			mp.latencyMatrix.UpdateLatency(int(metrics.RankLatencies.Rank), int(remoteRank), latency)
		}
	}

	return nil
}

// LatencyMatrix manages the node communication latencies
type LatencyMatrix struct {
	data [][]float64
	size int
	mu   sync.RWMutex
}

func NewLatencyMatrix(size int) *LatencyMatrix {
	matrix := make([][]float64, size)
	for i := range matrix {
		matrix[i] = make([]float64, size)
	}
	return &LatencyMatrix{
		data: matrix,
		size: size,
	}
}

func (lm *LatencyMatrix) UpdateLatency(from, to int, latency float64) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if from >= 0 && from < lm.size && to >= 0 && to < lm.size {
		lm.data[from][to] = latency
	}
}
