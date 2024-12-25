package main

import (
	"fmt"
	"math"
	"sync"
	"testing"
)

func TestNewLatencyMatrix(t *testing.T) {
	tests := []struct {
		name string
		size int
	}{
		{"zero size", 0},
		{"size one", 1},
		{"normal size", 4},
		{"large size", 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matrix := NewLatencyMatrix(tt.size)

			if len(matrix.Data) != tt.size {
				t.Errorf("Expected matrix size %d, got %d", tt.size, len(matrix.Data))
			}

			if len(matrix.NodeIDs) != tt.size {
				t.Errorf("Expected NodeIDs size %d, got %d", tt.size, len(matrix.NodeIDs))
			}

			// Check that all rows are initialized
			for i := 0; i < tt.size; i++ {
				if len(matrix.Data[i]) != tt.size {
					t.Errorf("Row %d: expected length %d, got %d", i, tt.size, len(matrix.Data[i]))
				}
			}
		})
	}
}

func TestLatencyMatrixUpdateLatency(t *testing.T) {
	tests := []struct {
		name     string
		size     int
		nodeIDs  []string
		updates  [][3]interface{} // [fromNode, toNode, latency]
		expected map[string]map[string]float64
	}{
		{
			name:    "simple update",
			size:    2,
			nodeIDs: []string{"node1", "node2"},
			updates: [][3]interface{}{
				{"node1", "node2", 100.0},
			},
			expected: map[string]map[string]float64{
				"node1": {"node2": 100.0},
			},
		},
		{
			name:    "multiple updates",
			size:    3,
			nodeIDs: []string{"node1", "node2", "node3"},
			updates: [][3]interface{}{
				{"node1", "node2", 100.0},
				{"node2", "node3", 150.0},
				{"node3", "node1", 200.0},
			},
			expected: map[string]map[string]float64{
				"node1": {"node2": 100.0},
				"node2": {"node3": 150.0},
				"node3": {"node1": 200.0},
			},
		},
		{
			name:    "update same pair",
			size:    2,
			nodeIDs: []string{"node1", "node2"},
			updates: [][3]interface{}{
				{"node1", "node2", 100.0},
				{"node1", "node2", 150.0}, // Should override
			},
			expected: map[string]map[string]float64{
				"node1": {"node2": 150.0},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matrix := NewLatencyMatrix(tt.size)
			copy(matrix.NodeIDs, tt.nodeIDs)

			// Apply updates
			for _, update := range tt.updates {
				matrix.UpdateLatency(
					update[0].(string),
					update[1].(string),
					update[2].(float64),
				)
			}

			// Verify results
			for fromNode, toNodes := range tt.expected {
				for toNode, expectedLatency := range toNodes {
					fromIdx := -1
					toIdx := -1
					for i, id := range matrix.NodeIDs {
						if id == fromNode {
							fromIdx = i
						}
						if id == toNode {
							toIdx = i
						}
					}
					if fromIdx == -1 || toIdx == -1 {
						t.Errorf("Node indices not found for %s->%s", fromNode, toNode)
						continue
					}
					if matrix.Data[fromIdx][toIdx] != expectedLatency {
						t.Errorf("Expected latency %f for %s->%s, got %f",
							expectedLatency, fromNode, toNode, matrix.Data[fromIdx][toIdx])
					}
				}
			}
		})
	}
}

func TestLatencyMatrixConcurrency(t *testing.T) {
	matrix := NewLatencyMatrix(3)
	matrix.NodeIDs = []string{"node1", "node2", "node3"}

	// Number of concurrent updates to perform
	numUpdates := 1000
	var wg sync.WaitGroup
	wg.Add(numUpdates)

	// Perform concurrent updates
	for i := 0; i < numUpdates; i++ {
		go func(i int) {
			defer wg.Done()
			fromNode := matrix.NodeIDs[i%len(matrix.NodeIDs)]
			toNode := matrix.NodeIDs[(i+1)%len(matrix.NodeIDs)]
			matrix.UpdateLatency(fromNode, toNode, float64(i))
		}(i)
	}

	wg.Wait()

	// Verify no data races occurred
	// Note: This doesn't verify correctness, just that the matrix wasn't corrupted
	for i := range matrix.Data {
		for j := range matrix.Data[i] {
			if math.IsNaN(matrix.Data[i][j]) {
				t.Errorf("Data corruption detected at [%d][%d]", i, j)
			}
		}
	}
}

func TestGetAverageLatencies(t *testing.T) {
	tests := []struct {
		name     string
		size     int
		nodeIDs  []string
		updates  [][3]interface{}
		expected map[string]float64
	}{
		{
			name:    "simple average",
			size:    2,
			nodeIDs: []string{"node1", "node2"},
			updates: [][3]interface{}{
				{"node1", "node2", 100.0},
				{"node2", "node1", 200.0},
			},
			expected: map[string]float64{
				"node1": 150.0, // Average of sending and receiving
				"node2": 150.0,
			},
		},
		{
			name:    "complex average",
			size:    3,
			nodeIDs: []string{"node1", "node2", "node3"},
			updates: [][3]interface{}{
				{"node1", "node2", 100.0},
				{"node2", "node3", 150.0},
				{"node3", "node1", 200.0},
				{"node2", "node1", 120.0},
				{"node3", "node2", 180.0},
				{"node1", "node3", 160.0},
			},
			expected: map[string]float64{
				"node1": 160.0, // (100 + 120 + 160 + 200) / 4
				"node2": 150.0, // (150 + 180 + 100 + 120) / 4
				"node3": 180.0, // (200 + 160 + 150 + 180) / 4
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matrix := NewLatencyMatrix(tt.size)
			copy(matrix.NodeIDs, tt.nodeIDs)

			// Apply updates
			for _, update := range tt.updates {
				matrix.UpdateLatency(
					update[0].(string),
					update[1].(string),
					update[2].(float64),
				)
			}

			averages := matrix.GetAverageLatencies()

			// Verify results
			for nodeID, expectedAvg := range tt.expected {
				if avg, exists := averages[nodeID]; !exists {
					t.Errorf("Expected average for node %s not found", nodeID)
				} else {
					// Use small epsilon for float comparison
					if math.Abs(avg-expectedAvg) > 0.001 {
						t.Errorf("Expected average %f for node %s, got %f",
							expectedAvg, nodeID, avg)
					}
				}
			}
		})
	}
}

func TestResizeMatrix(t *testing.T) {
	tests := []struct {
		name        string
		initialSize int
		newNodeIDs  []string
		updates     [][3]interface{}
		preserved   [][3]interface{} // Values that should be preserved after resize
	}{
		{
			name:        "grow matrix",
			initialSize: 2,
			newNodeIDs:  []string{"node1", "node2", "node3"},
			updates: [][3]interface{}{
				{"node1", "node2", 100.0},
			},
			preserved: [][3]interface{}{
				{"node1", "node2", 100.0},
			},
		},
		{
			name:        "shrink matrix",
			initialSize: 3,
			newNodeIDs:  []string{"node1", "node2"},
			updates: [][3]interface{}{
				{"node1", "node2", 100.0},
				{"node2", "node3", 150.0}, // Should be removed
			},
			preserved: [][3]interface{}{
				{"node1", "node2", 100.0},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Initialize matrix
			matrix := NewLatencyMatrix(tt.initialSize)
			matrix.NodeIDs = make([]string, tt.initialSize)
			for i := 0; i < tt.initialSize; i++ {
				matrix.NodeIDs[i] = fmt.Sprintf("node%d", i+1)
			}

			// Apply initial updates
			for _, update := range tt.updates {
				matrix.UpdateLatency(
					update[0].(string),
					update[1].(string),
					update[2].(float64),
				)
			}

			// Resize matrix
			matrix.ResizeMatrix(tt.newNodeIDs)

			// Verify size
			if len(matrix.Data) != len(tt.newNodeIDs) {
				t.Errorf("Expected matrix size %d, got %d",
					len(tt.newNodeIDs), len(matrix.Data))
			}

			// Verify preserved values
			for _, preserved := range tt.preserved {
				fromNode := preserved[0].(string)
				toNode := preserved[1].(string)
				expectedLatency := preserved[2].(float64)

				fromIdx := -1
				toIdx := -1
				for i, id := range matrix.NodeIDs {
					if id == fromNode {
						fromIdx = i
					}
					if id == toNode {
						toIdx = i
					}
				}

				if fromIdx == -1 || toIdx == -1 {
					t.Errorf("Expected nodes not found after resize: %s->%s",
						fromNode, toNode)
					continue
				}

				if matrix.Data[fromIdx][toIdx] != expectedLatency {
					t.Errorf("Expected preserved latency %f for %s->%s, got %f",
						expectedLatency, fromNode, toNode, matrix.Data[fromIdx][toIdx])
				}
			}
		})
	}
}

// Benchmark tests
func BenchmarkLatencyMatrixUpdate(b *testing.B) {
	matrix := NewLatencyMatrix(100)
	for i := 0; i < 100; i++ {
		matrix.NodeIDs[i] = fmt.Sprintf("node%d", i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fromNode := fmt.Sprintf("node%d", i%100)
		toNode := fmt.Sprintf("node%d", (i+1)%100)
		matrix.UpdateLatency(fromNode, toNode, float64(i))
	}
}

func BenchmarkGetAverageLatencies(b *testing.B) {
	matrix := NewLatencyMatrix(100)
	for i := 0; i < 100; i++ {
		matrix.NodeIDs[i] = fmt.Sprintf("node%d", i)
		for j := 0; j < 100; j++ {
			if i != j {
				matrix.Data[i][j] = float64(i + j)
			}
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matrix.GetAverageLatencies()
	}
}
