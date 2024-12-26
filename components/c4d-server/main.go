package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"sync"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"

	"c4d-server/proto"
)

const (
	keepAliveInterval = 3 * time.Second
	latencyThreshold  = 100 * time.Millisecond
)

type ServerConfig struct {
	ExpectedNodes int
	StandbyNodes  int
	ActiveNodes   int // ExpectedNodes - StandbyNodes
}

// Core data structures
type NodeState struct {
	NodeURL       string
	Status        string // "active", "standby", "failed"
	LastHeartbeat time.Time
	Rank          int
	ProcessID     int64
	SystemMetrics struct {
		CPUUsage    float64
		MemoryUsage float64
		LastUpdated time.Time
	}
}

type LatencyMatrix struct {
	Data  [][]float64
	Size  int
	mutex sync.RWMutex
}

type C4DServer struct {
	c4d.UnimplementedC4DServiceServer
	nodes         map[string]*NodeState
	latencyMatrix *LatencyMatrix
	standbyNodes  []string
	worldSize     int
	mutex         sync.RWMutex
	config        ServerConfig
}

// LatencyMatrix methods
func NewLatencyMatrix(size int) *LatencyMatrix {
	matrix := &LatencyMatrix{
		Data: make([][]float64, size),
		Size: size,
	}
	for i := range matrix.Data {
		matrix.Data[i] = make([]float64, size)
	}
	return matrix
}

func (m *LatencyMatrix) UpdateLatency(fromRank, toRank int, latency float64) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if fromRank >= 0 && fromRank < m.Size && toRank >= 0 && toRank < m.Size {
		m.Data[fromRank][toRank] = latency
		//fmt.Printf("Matrix updated: [%d][%d] = %.2f (matrix size: %d)\n",
		//	fromRank, toRank, latency, m.Size)
	} else {
		fmt.Printf("Invalid rank indices: [%d][%d] for matrix size %d\n",
			fromRank, toRank, m.Size)
	}
}

func (m *LatencyMatrix) GetAverageLatencies() map[int]float64 {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	averages := make(map[int]float64)
	for i := 0; i < m.Size; i++ {
		var sum, count float64
		for j := 0; j < m.Size; j++ {
			if i != j && m.Data[i][j] > 0 {
				sum += m.Data[i][j]
				count++
			}
		}
		if count > 0 {
			averages[i] = sum / count
		}
	}
	return averages
}

func (m *LatencyMatrix) ResizeMatrix(newSize int) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	newData := make([][]float64, newSize)
	for i := range newData {
		newData[i] = make([]float64, newSize)
	}

	// Copy existing data where possible
	minSize := m.Size
	if newSize < minSize {
		minSize = newSize
	}
	for i := 0; i < minSize; i++ {
		copy(newData[i], m.Data[i])
	}

	m.Data = newData
	m.Size = newSize
}

func NewC4DServer(expectedNodes, standbyNodes int) *C4DServer {
	if standbyNodes >= expectedNodes {
		panic("Number of standby nodes must be less than total expected nodes")
	}

	config := ServerConfig{
		ExpectedNodes: expectedNodes,
		StandbyNodes:  standbyNodes,
		ActiveNodes:   expectedNodes - standbyNodes,
	}

	return &C4DServer{
		nodes:         make(map[string]*NodeState),
		latencyMatrix: NewLatencyMatrix(expectedNodes - standbyNodes), // Only size for active nodes
		standbyNodes:  make([]string, 0, standbyNodes),
		worldSize:     expectedNodes - standbyNodes,
		config:        config,
	}
}

// Core monitoring functionality
func (s *C4DServer) monitorNodes() {
	ticker := time.NewTicker(keepAliveInterval)
	defer ticker.Stop()

	latencyPrintTicker := time.NewTicker(500 * time.Millisecond)
	defer latencyPrintTicker.Stop()

	for {
		select {
		case <-ticker.C:
			s.checkNodeHealth()
			s.analyzeLatencies()
		case <-latencyPrintTicker.C:
			s.latencyMatrix.PrintMatrix()
		}
	}
}

func (s *C4DServer) checkNodeHealth() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	now := time.Now()
	for nodeID, state := range s.nodes {
		if state.Status == "active" && now.Sub(state.LastHeartbeat) > keepAliveInterval {
			fmt.Printf("Node %s exceeded keepalive interval\n", nodeID)
			s.handleNodeFailure(nodeID)
		}
	}
}

func (s *C4DServer) analyzeLatencies() {
	averageLatencies := s.latencyMatrix.GetAverageLatencies()
	if len(averageLatencies) == 0 {
		return
	}

	var total, count float64
	for _, latency := range averageLatencies {
		total += latency
		count++
	}
	mean := total / count

	var sumSquaredDiff float64
	for _, latency := range averageLatencies {
		diff := latency - mean
		sumSquaredDiff += diff * diff
	}
	stdDev := math.Sqrt(sumSquaredDiff / count)
	threshold := mean + 2*stdDev

	// Check for problematic nodes
	for rank, latency := range averageLatencies {
		if latency > threshold {
			fmt.Printf("Rank %d has high latency: %.2fms (threshold: %.2fms)\n",
				rank, latency, threshold)

			// Find nodeID for this rank
			var nodeID string
			for id, state := range s.nodes {
				if state.Rank == rank {
					nodeID = id
					break
				}
			}

			if nodeID != "" {
				s.mutex.Lock()
				s.handleNodeFailure(nodeID)
				s.mutex.Unlock()
			}
		}
	}
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

func (s *C4DServer) notifyNodeOfRestart(node *NodeState) error {
	// Prepare configuration for node
	activeNodes := make([]map[string]interface{}, 0)
	for nodeID, state := range s.nodes {
		if state.Status == "active" {
			activeNodes = append(activeNodes, map[string]interface{}{
				"node_id": nodeID,
				"rank":    state.Rank,
				"url":     state.NodeURL,
			})
		}
	}

	config := map[string]interface{}{
		"new_rank":     node.Rank,
		"world_size":   s.worldSize,
		"active_nodes": activeNodes,
	}

	// Make HTTP POST request to node's restart endpoint
	client := &http.Client{Timeout: 10 * time.Second}
	jsonData, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	resp, err := client.Post(
		fmt.Sprintf("%s/restart", node.NodeURL),
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return fmt.Errorf("failed to notify node: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("node returned status: %d", resp.StatusCode)
	}

	return nil
}

func (s *C4DServer) handleNodeFailure(failedNodeID string) {
	// Already under mutex lock from caller

	failedNode := s.nodes[failedNodeID]
	if failedNode == nil {
		return
	}

	fmt.Printf("Handling failure of node %s (rank %d)\n", failedNodeID, failedNode.Rank)

	// 1. Mark node as permanently failed
	failedNode.Status = "failed"
	failedRank := failedNode.Rank
	failedNode.Rank = -1

	// 2. Try to replace with standby node
	if len(s.standbyNodes) == 0 {
		fmt.Printf("No standby nodes available. World size will decrease.\n")
		s.worldSize--
		s.latencyMatrix.ResizeMatrix(s.worldSize)
		s.reassignRanks()
		return
	}

	// Get standby node
	standbyID := s.standbyNodes[0]
	s.standbyNodes = s.standbyNodes[1:]
	standbyNode := s.nodes[standbyID]

	if standbyNode == nil {
		fmt.Printf("Standby node %s not found in node list\n", standbyID)
		return
	}

	fmt.Printf("Activating standby node %s to replace failed node\n", standbyID)

	// 3. Activate standby node
	standbyNode.Status = "active"
	standbyNode.Rank = failedRank // Initially assign failed node's rank

	// 4. Reassign ranks and notify all active nodes
	s.reassignRanks()
}

func (m *LatencyMatrix) PrintMatrix() {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	fmt.Printf("   |")
	for i := 0; i < m.Size; i++ {
		fmt.Printf(" %3d |", i)
	}
	fmt.Println()

	fmt.Print("---+")
	for i := 0; i < m.Size; i++ {
		fmt.Print("-----+")
	}
	fmt.Println()

	for i := 0; i < m.Size; i++ {
		fmt.Printf("%3d|", i)
		for j := 0; j < m.Size; j++ {
			if m.Data[i][j] == 0 {
				fmt.Print("   - |")
			} else {
				fmt.Printf("%4.1f|", m.Data[i][j])
			}
		}
		fmt.Println()
	}
	fmt.Println()
}

func (s *C4DServer) reassignRanks() {
	// Get active nodes
	activeNodes := make([]*NodeState, 0)
	for _, state := range s.nodes {
		if state.Status == "active" {
			activeNodes = append(activeNodes, state)
		}
	}

	// Sort by nodeURL for consistent rank assignment
	sort.Slice(activeNodes, func(i, j int) bool {
		return activeNodes[i].NodeURL < activeNodes[j].NodeURL
	})

	// Assign new ranks
	for i, node := range activeNodes {
		node.Rank = i
	}

	// Prepare configuration for all nodes
	activeNodesInfo := make([]map[string]interface{}, 0, len(activeNodes))
	for _, node := range activeNodes {
		activeNodesInfo = append(activeNodesInfo, map[string]interface{}{
			"node_id": node.NodeURL,
			"rank":    node.Rank,
			"url":     node.NodeURL,
		})
	}

	// Notify all active nodes of new configuration
	for _, node := range activeNodes {
		config := map[string]interface{}{
			"new_rank":     node.Rank,
			"world_size":   s.worldSize,
			"active_nodes": activeNodesInfo,
		}

		// Send configuration to node with retries
		go func(node *NodeState, cfg map[string]interface{}) {
			maxRetries := 3
			for retry := 0; retry < maxRetries; retry++ {
				if err := s.notifyNodeAndWaitForRestart(node, cfg); err != nil {
					fmt.Printf("Failed to notify node %s (attempt %d): %v\n",
						node.NodeURL, retry+1, err)
					time.Sleep(time.Second * time.Duration(retry+1))
					continue
				}
				fmt.Printf("Successfully notified node %s of new configuration\n", node.NodeURL)
				break
			}
		}(node, config)
	}
}

func (s *C4DServer) notifyNodeAndWaitForRestart(node *NodeState, config map[string]interface{}) error {
	client := &http.Client{Timeout: 30 * time.Second} // Longer timeout for restart

	jsonData, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Send restart configuration
	resp, err := client.Post(
		fmt.Sprintf("%s/restart", node.NodeURL),
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return fmt.Errorf("failed to send restart config: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("node returned status %d: %s", resp.StatusCode, string(body))
	}

	// Wait for node to acknowledge restart
	time.Sleep(5 * time.Second) // Give node time to restart

	// Verify node is responsive
	healthResp, err := client.Get(fmt.Sprintf("%s/metrics", node.NodeURL))
	if err != nil {
		return fmt.Errorf("failed to verify node health after restart: %w", err)
	}
	defer healthResp.Body.Close()

	if healthResp.StatusCode != http.StatusOK {
		return fmt.Errorf("node health check failed after restart: %d", healthResp.StatusCode)
	}

	return nil
}

func (s *C4DServer) Register(ctx context.Context, req *c4d.RegisterRequest) (*c4d.RegisterResponse, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Check if the node is already registered and marked as failed
	node := s.nodes[req.NodeId]
	if node != nil && node.Status == "failed" {
		fmt.Printf("Node %s is marked as failed. Reconnecting...\n", req.NodeId)
		node.Status = "active"
		node.LastHeartbeat = time.Now()
		return &c4d.RegisterResponse{
			Message: "Node reconnected successfully",
			NodeId:  req.NodeId,
		}, nil
	}

	// Check if we've exceeded expected nodes
	totalNodes := len(s.nodes)
	if totalNodes >= s.config.ExpectedNodes {
		return nil, fmt.Errorf("maximum number of nodes (%d) reached", s.config.ExpectedNodes)
	}

	nodeState := &NodeState{
		NodeURL:       req.NodeUrl,
		Status:        req.NodeStatus,
		LastHeartbeat: time.Now(),
		Rank:          -1,
	}

	// Count current active nodes
	activeCount := 0
	for _, state := range s.nodes {
		if state.Status == "active" {
			activeCount++
		}
	}

	// If we haven't reached the desired number of active nodes yet,
	// force this node to be active regardless of requested status
	if activeCount < s.config.ActiveNodes {
		nodeState.Status = "active"
		nodeState.Rank = activeCount
	} else {
		// If we have all our active nodes, this must be a standby node
		if len(s.standbyNodes) >= s.config.StandbyNodes {
			return nil, fmt.Errorf("maximum number of standby nodes (%d) reached", s.config.StandbyNodes)
		}
		nodeState.Status = "standby"
		s.standbyNodes = append(s.standbyNodes, req.NodeId)
	}

	s.nodes[req.NodeId] = nodeState
	fmt.Printf("Registered node %s as %s with rank %d\n",
		req.NodeId, nodeState.Status, nodeState.Rank)

	return &c4d.RegisterResponse{
		Message: "Node registered successfully",
		NodeId:  req.NodeId,
	}, nil
}

func (s *C4DServer) SendMetrics(ctx context.Context, req *c4d.MetricsRequest) (*c4d.MetricsResponse, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	node := s.nodes[req.NodeId]
	if node == nil {
		return nil, fmt.Errorf("unknown node: %s", req.NodeId)
	}

	// Check if the node is marked as failed
	if node.Status == "failed" {
		return nil, fmt.Errorf("node %s is marked as failed", req.NodeId)
	}

	// Ignore metrics from standby nodes
	if node.Status == "standby" {
		return &c4d.MetricsResponse{Message: "Standby node metrics ignored"}, nil
	}

	// Update node state with the current timestamp
	node.LastHeartbeat = time.Now()
	node.SystemMetrics.CPUUsage = req.Metrics.CpuUsage
	node.SystemMetrics.MemoryUsage = req.Metrics.RamUsage
	node.SystemMetrics.LastUpdated = time.Now()

	if req.Status != nil {
		if !req.Status.IsAlive {
			s.handleNodeFailure(req.NodeId)
			return &c4d.MetricsResponse{Message: "Node failure processed"}, nil
		}
		node.ProcessID = req.Status.ProcessId
	}

	// Update latency matrix with more detailed logging
	if req.Metrics.RankLatencies != nil {
		senderRank := node.Rank
		for remoteRank, avgLatency := range req.Metrics.RankLatencies.AvgLatencies {
			s.latencyMatrix.UpdateLatency(senderRank, int(remoteRank), avgLatency)
		}
	}

	return &c4d.MetricsResponse{Message: "Metrics processed"}, nil
}

func main() {
	expectedNodesStr := os.Getenv("EXPECTED_NODES")
	if expectedNodesStr == "" {
		fmt.Println("ERROR: EXPECTED_NODES environment variable is required")
		os.Exit(1)
	}

	standbyNodesStr := os.Getenv("STANDBY_NODES")
	if standbyNodesStr == "" {
		fmt.Println("ERROR: STANDBY_NODES environment variable is required")
		os.Exit(1)
	}

	expectedNodes, err := strconv.Atoi(expectedNodesStr)
	if err != nil {
		fmt.Printf("ERROR: Invalid value for EXPECTED_NODES: %s\n", expectedNodesStr)
		os.Exit(1)
	}

	standbyNodes, err := strconv.Atoi(standbyNodesStr)
	if err != nil {
		fmt.Printf("ERROR: Invalid value for STANDBY_NODES: %s\n", standbyNodesStr)
		os.Exit(1)
	}

	server := NewC4DServer(expectedNodes, standbyNodes)
	fmt.Printf("Starting server with configuration:\n")
	fmt.Printf("Expected nodes: %d\n", server.config.ExpectedNodes)
	fmt.Printf("Standby nodes: %d\n", server.config.StandbyNodes)
	fmt.Printf("Active nodes: %d\n", server.config.ActiveNodes)

	// Start monitoring
	go server.monitorNodes()

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
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		<-sigCh
		fmt.Println("\nGracefully stopping server...")
		s.GracefulStop()
	}()

	go func() {
		// call handleNodeFailure(0) after 10 seconds to simulate a node failure
		time.Sleep(10 * time.Second)
		fmt.Printf("\nSimulating node failure...\n")
		server.handleNodeFailure("127.0.0")
	}()

	fmt.Println("Starting gRPC server on :8091")
	if err := s.Serve(lis); err != nil {
		fmt.Printf("Failed to serve: %v\n", err)
	}

}
