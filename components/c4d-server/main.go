package main

import (
	"bytes"
	"c4d-server/proto"
	"context"
	"encoding/json"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"sync"
	"syscall"
	"time"
)

// LatencyMatrix stores communication latencies between nodes
type LatencyMatrix struct {
	Data    [][]float64
	NodeIDs []string // To map matrix indices to node IDs
	mutex   sync.RWMutex
}

// SystemMetrics stores the current system metrics for a node
type SystemMetrics struct {
	CPUUsage    float64
	MemoryUsage float64
	LastUpdated time.Time
}

// NodeState tracks the complete state of a node
type NodeState struct {
	NodeURL       string
	Status        string // "active", "standby", "failed"
	LastHeartbeat time.Time
	Rank          int
	SystemMetrics struct {
		CPUUsage    float64
		MemoryUsage float64
		LastUpdated time.Time
	}
	ProcessID int64
}

// C4DServer holds the state of the C4D server
type C4DServer struct {
	c4d.UnimplementedC4DServiceServer
	nodes             map[string]*NodeState // nodeID -> NodeState
	latencyMatrix     *LatencyMatrix
	standbyNodes      []string // List of standby node IDs
	worldSize         int
	keepAliveInterval time.Duration
	latencyThreshold  time.Duration // Threshold for high latency
	mutex             sync.RWMutex
}

func NewLatencyMatrix(size int) *LatencyMatrix {
	matrix := &LatencyMatrix{
		Data:    make([][]float64, size),
		NodeIDs: make([]string, size),
	}
	for i := range matrix.Data {
		matrix.Data[i] = make([]float64, size)
	}
	return matrix
}

func NewC4DServer() *C4DServer {
	return &C4DServer{
		nodes:             make(map[string]*NodeState),
		latencyMatrix:     NewLatencyMatrix(0), // Will resize as nodes join
		standbyNodes:      make([]string, 0),
		keepAliveInterval: 5 * time.Second,
		latencyThreshold:  100 * time.Millisecond,
		worldSize:         0,
	}
}

// UpdateLatency updates the latency matrix with a new measurement
func (m *LatencyMatrix) UpdateLatency(fromNode, toNode string, latency float64) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	fromIdx := -1
	toIdx := -1

	// Find indices for the nodes
	for i, id := range m.NodeIDs {
		if id == fromNode {
			fromIdx = i
		}
		if id == toNode {
			toIdx = i
		}
	}

	if fromIdx >= 0 && toIdx >= 0 {
		m.Data[fromIdx][toIdx] = latency
	}
}

// GetAverageLatencies returns average latencies for each node
func (m *LatencyMatrix) GetAverageLatencies() map[string]float64 {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	averages := make(map[string]float64)

	for i, nodeID := range m.NodeIDs {
		var sum, count float64

		// Check row (outgoing latencies)
		for j := range m.NodeIDs {
			if i != j && m.Data[i][j] > 0 {
				sum += m.Data[i][j]
				count++
			}
		}

		// Check column (incoming latencies)
		for j := range m.NodeIDs {
			if i != j && m.Data[j][i] > 0 {
				sum += m.Data[j][i]
				count++
			}
		}

		if count > 0 {
			averages[nodeID] = sum / count
		}
	}

	return averages
}

// ResizeMatrix resizes the latency matrix when nodes join or leave
func (m *LatencyMatrix) ResizeMatrix(nodeIDs []string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	newSize := len(nodeIDs)
	newData := make([][]float64, newSize)
	for i := range newData {
		newData[i] = make([]float64, newSize)
	}

	// Copy existing data
	for i, oldID := range m.NodeIDs {
		for j, oldID2 := range m.NodeIDs {
			newI := -1
			newJ := -1

			// Find new indices
			for k, newID := range nodeIDs {
				if newID == oldID {
					newI = k
				}
				if newID == oldID2 {
					newJ = k
				}
			}

			if newI >= 0 && newJ >= 0 {
				newData[newI][newJ] = m.Data[i][j]
			}
		}
	}

	m.Data = newData
	m.NodeIDs = nodeIDs
}

func (s *C4DServer) getActiveNodeIDs() []string {
	activeIDs := make([]string, 0)
	for nodeID, state := range s.nodes {
		if state.Status == "active" {
			activeIDs = append(activeIDs, nodeID)
		}
	}
	return activeIDs
}

// notifyNodeOfRestart sends a notification to a node about the new configuration
func (s *C4DServer) notifyNodeOfRestart(node *NodeState) error {
	// Prepare the new configuration
	config := map[string]interface{}{
		"new_rank":     node.Rank,
		"world_size":   s.worldSize,
		"active_nodes": make([]map[string]interface{}, 0),
	}

	// Add information about all active nodes
	for nodeID, state := range s.nodes {
		if state.Status == "active" {
			config["active_nodes"] = append(config["active_nodes"].([]map[string]interface{}), map[string]interface{}{
				"node_id": nodeID,
				"rank":    state.Rank,
				"url":     state.NodeURL,
			})
		}
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:       100,
			IdleConnTimeout:    90 * time.Second,
			DisableCompression: true,
			ForceAttemptHTTP2:  true,
		},
	}

	// Marshal configuration to JSON
	jsonData, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Create request context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST",
		fmt.Sprintf("%s/restart", node.NodeURL),
		bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Send request with retries
	maxRetries := 3
	for retry := 0; retry < maxRetries; retry++ {
		resp, err := client.Do(req)
		if err != nil {
			if retry == maxRetries-1 {
				return fmt.Errorf("failed to notify node after %d retries: %w", maxRetries, err)
			}
			time.Sleep(time.Second * time.Duration(retry+1))
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			if retry == maxRetries-1 {
				return fmt.Errorf("node returned non-OK status: %d, body: %s", resp.StatusCode, string(body))
			}
			time.Sleep(time.Second * time.Duration(retry+1))
			continue
		}

		// Successfully notified node
		fmt.Printf("Successfully notified node %s of new configuration (rank: %d)\n",
			node.NodeURL, node.Rank)
		return nil
	}

	return fmt.Errorf("failed to notify node after all retries")
}

// Helper function to log node configuration changes
func (s *C4DServer) logConfigChange(oldConfig, newConfig map[string]interface{}) {
	fmt.Printf("Configuration change detected:\n")
	fmt.Printf("World size: %d\n", s.worldSize)
	fmt.Printf("Active nodes:\n")
	for nodeID, state := range s.nodes {
		if state.Status == "active" {
			fmt.Printf("  - Node %s: Rank %d, URL %s\n",
				nodeID, state.Rank, state.NodeURL)
		}
	}
	fmt.Printf("Standby nodes:\n")
	for _, nodeID := range s.standbyNodes {
		if state, exists := s.nodes[nodeID]; exists {
			fmt.Printf("  - Node %s: URL %s\n",
				nodeID, state.NodeURL)
		}
	}
}

// UpdateWorldSize updates the world size based on active nodes
func (s *C4DServer) UpdateWorldSize() {
	activeCount := 0
	for _, state := range s.nodes {
		if state.Status == "active" {
			activeCount++
		}
	}
	s.worldSize = activeCount
}

// ValidateConfiguration checks if the current configuration is valid
func (s *C4DServer) ValidateConfiguration() error {
	// Check if we have enough active nodes
	activeNodes := s.getActiveNodeIDs()
	if len(activeNodes) != s.worldSize {
		return fmt.Errorf("mismatch between active nodes (%d) and world size (%d)",
			len(activeNodes), s.worldSize)
	}

	// Check if all active nodes have valid ranks
	ranksSeen := make(map[int]bool)
	for _, nodeID := range activeNodes {
		state := s.nodes[nodeID]
		if state.Rank < 0 || state.Rank >= s.worldSize {
			return fmt.Errorf("node %s has invalid rank: %d", nodeID, state.Rank)
		}
		if ranksSeen[state.Rank] {
			return fmt.Errorf("duplicate rank %d detected", state.Rank)
		}
		ranksSeen[state.Rank] = true
	}

	// Check if all ranks are assigned
	for i := 0; i < s.worldSize; i++ {
		if !ranksSeen[i] {
			return fmt.Errorf("rank %d is not assigned to any node", i)
		}
	}

	return nil
}

// CheckNodeHealth checks if any nodes have exceeded the keepalive interval
func (s *C4DServer) CheckNodeHealth() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	now := time.Now()
	for nodeID, state := range s.nodes {
		if state.Status == "active" && now.Sub(state.LastHeartbeat) > s.keepAliveInterval {
			fmt.Printf("Node %s exceeded keepalive interval. Marking as failed.\n", nodeID)
			s.handleNodeFailure(nodeID)
		}
	}
}

// CheckLatencyMatrix analyzes the latency matrix for problematic nodes
func (s *C4DServer) CheckLatencyMatrix() {
	s.mutex.RLock()
	averageLatencies := s.latencyMatrix.GetAverageLatencies()
	s.mutex.RUnlock()

	if len(averageLatencies) == 0 {
		return // No latency data yet
	}

	// Calculate overall average latency
	var totalLatency, count float64
	for _, latency := range averageLatencies {
		totalLatency += latency
		count++
	}
	overallAvg := totalLatency / count

	// Calculate standard deviation
	var sumSquaredDiff float64
	for _, latency := range averageLatencies {
		diff := latency - overallAvg
		sumSquaredDiff += diff * diff
	}
	stdDev := math.Sqrt(sumSquaredDiff / count)

	// Check for nodes with significantly higher latency
	// Using 2 standard deviations as a threshold
	threshold := overallAvg + 2*stdDev

	for nodeID, latency := range averageLatencies {
		if latency > threshold {
			fmt.Printf("Node %s has significantly higher latency: %.2fms (avg: %.2fms, threshold: %.2fms)\n",
				nodeID, latency, overallAvg, threshold)
			s.handleNodeFailure(nodeID)
		}
	}
}

// handleNodeFailure manages the process of replacing a failed node
func (s *C4DServer) handleNodeFailure(failedNodeID string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	failedNode := s.nodes[failedNodeID]
	if failedNode == nil {
		return
	}

	// Find a standby node
	var standbyNodeID string
	if len(s.standbyNodes) > 0 {
		standbyNodeID = s.standbyNodes[0]
		s.standbyNodes = s.standbyNodes[1:]
	} else {
		fmt.Printf("No standby nodes available to replace failed node %s\n", failedNodeID)
		return
	}

	// Update node states
	failedNode.Status = "failed"
	standbyNode := s.nodes[standbyNodeID]
	standbyNode.Status = "active"
	standbyNode.Rank = failedNode.Rank

	// Trigger rank reassignment and computation restart
	s.reassignRanksAndRestart()
}

// reassignRanksAndRestart manages the process of restarting computation
func (s *C4DServer) reassignRanksAndRestart() {
	// Get active nodes
	activeNodes := make([]*NodeState, 0)
	for _, state := range s.nodes {
		if state.Status == "active" {
			activeNodes = append(activeNodes, state)
		}
	}

	// Sort by nodeID to ensure consistent rank assignment
	sort.Slice(activeNodes, func(i, j int) bool {
		return activeNodes[i].NodeURL < activeNodes[j].NodeURL
	})

	// Assign ranks
	for i, node := range activeNodes {
		node.Rank = i
	}

	// Notify all active nodes of new configuration
	for _, node := range activeNodes {
		s.notifyNodeOfRestart(node)
	}
}
func (s *C4DServer) SendMetrics(ctx context.Context, req *c4d.MetricsRequest) (*c4d.MetricsResponse, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	node := s.nodes[req.NodeId]
	if node == nil {
		return nil, fmt.Errorf("unknown node: %s", req.NodeId)
	}

	// Update node state with essential information
	node.LastHeartbeat = time.Now()
	node.SystemMetrics.CPUUsage = req.Metrics.CpuUsage
	node.SystemMetrics.MemoryUsage = req.Metrics.RamUsage
	node.SystemMetrics.LastUpdated = time.Now()

	if req.Status != nil {
		if !req.Status.IsAlive {
			// Handle process failure
			fmt.Printf("Node %s reported process failure\n", req.NodeId)
			s.handleNodeFailure(req.NodeId)
			return &c4d.MetricsResponse{Message: "Node failure processed"}, nil
		}
		node.ProcessID = req.Status.ProcessId
	}

	// Update latency matrix with pre-averaged values
	if req.Metrics.RankLatencies != nil {
		senderRank := req.Metrics.RankLatencies.Rank

		// Find sender's node ID (we already have it, but let's verify rank mapping)
		var senderNodeID string
		for nID, state := range s.nodes {
			if state.Rank == int(senderRank) {
				senderNodeID = nID
				break
			}
		}

		// Update latency matrix with averaged values
		for receiverRank, avgLatency := range req.Metrics.RankLatencies.AvgLatencies {
			// Find receiver's node ID
			var receiverNodeID string
			for nID, state := range s.nodes {
				if state.Rank == int(receiverRank) {
					receiverNodeID = nID
					break
				}
			}

			if senderNodeID != "" && receiverNodeID != "" {
				s.latencyMatrix.UpdateLatency(senderNodeID, receiverNodeID, avgLatency)
			}
		}
	}

	return &c4d.MetricsResponse{Message: "Metrics processed"}, nil
}

func (s *C4DServer) Register(ctx context.Context, req *c4d.RegisterRequest) (*c4d.RegisterResponse, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Create new node state
	nodeState := &NodeState{
		NodeURL:       req.NodeUrl,
		Status:        req.NodeStatus,
		LastHeartbeat: time.Now(),
		Rank:          -1, // Will be assigned if/when activated
	}

	s.nodes[req.NodeId] = nodeState

	if req.NodeStatus == "standby" {
		s.standbyNodes = append(s.standbyNodes, req.NodeId)
	} else {
		s.worldSize++
		s.latencyMatrix.ResizeMatrix(s.getActiveNodeIDs())
	}

	return &c4d.RegisterResponse{
		Message: "Node registered successfully",
		NodeId:  req.NodeId,
	}, nil
}

func (s *C4DServer) GetLatencyMatrix() map[string]map[string]float64 {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	result := make(map[string]map[string]float64)
	for i, fromNode := range s.latencyMatrix.NodeIDs {
		result[fromNode] = make(map[string]float64)
		for j, toNode := range s.latencyMatrix.NodeIDs {
			if s.latencyMatrix.Data[i][j] > 0 {
				result[fromNode][toNode] = s.latencyMatrix.Data[i][j]
			}
		}
	}

	return result
}

func (s *C4DServer) GetLatencyStats() map[string]interface{} {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	averageLatencies := s.latencyMatrix.GetAverageLatencies()

	var totalLatency, count float64
	for _, latency := range averageLatencies {
		totalLatency += latency
		count++
	}

	stats := map[string]interface{}{
		"matrix":          s.GetLatencyMatrix(),
		"node_averages":   averageLatencies,
		"overall_average": totalLatency / count,
	}

	return stats
}

func main() {
	server := NewC4DServer()

	// Start monitoring routines
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		for range ticker.C {
			server.CheckNodeHealth()
			server.CheckLatencyMatrix()
		}
	}()

	// Setup gRPC server
	lis, err := net.Listen("tcp", ":8091")
	if err != nil {
		fmt.Printf("Failed to listen: %v\n", err)
		return
	}

	opts := []grpc.ServerOption{
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     15 * time.Minute,
			MaxConnectionAge:      30 * time.Minute,
			MaxConnectionAgeGrace: 5 * time.Second,
			Time:                  5 * time.Second,
			Timeout:               1 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             5 * time.Second,
			PermitWithoutStream: true,
		}),
	}

	s := grpc.NewServer(opts...)
	c4d.RegisterC4DServiceServer(s, server)

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		<-sigCh

		fmt.Println("\nReceived shutdown signal. Gracefully stopping server...")
		s.GracefulStop()
	}()

	fmt.Println("Starting gRPC server on :8091")
	if err := s.Serve(lis); err != nil {
		fmt.Printf("Failed to serve: %v\n", err)
		return
	}
}
