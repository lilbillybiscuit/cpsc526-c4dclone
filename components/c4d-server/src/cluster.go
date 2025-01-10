package src

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"
)

type ClusterManager struct {
	server *C4DServer
	mu     MonitoredRWMutex
}

func NewClusterManager(server *C4DServer) *ClusterManager {
	return &ClusterManager{
		server: server,
	}
}

func (cm *ClusterManager) reconfigureCluster() error {
	if cm.server.state.Current() != Reconfiguring {
		if err := cm.server.state.Transition(Reconfiguring); err != nil {
			return fmt.Errorf("failed to transition to reconfiguring state: %w", err)
		}
	}

	maxAttempts := 3
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			cm.server.logger.Printf("Retrying cluster reconfiguration (attempt %d/%d)", attempt+1, maxAttempts)
			time.Sleep(time.Second * time.Duration(attempt+1))
		}

		// Get both active and standby nodes
		cm.server.registry.mu.RLock()
		var activeNodes, standbyNodes []*NodeState
		for _, node := range cm.server.registry.nodes {
			if node.Status == "active" {
				activeNodes = append(activeNodes, node)
			} else if node.Status == "standby" {
				standbyNodes = append(standbyNodes, node)
			}
		}
		cm.server.registry.mu.RUnlock()

		cm.server.logger.Printf("Current cluster status: %d active, %d standby nodes (%d required active nodes)",
			len(activeNodes), len(standbyNodes), cm.server.config.ActiveNodes)

		// If we don't have enough active nodes, try to promote standby nodes
		neededNodes := cm.server.config.ActiveNodes - len(activeNodes)
		if neededNodes > 0 && len(standbyNodes) > 0 {
			numToPromote := min(neededNodes, len(standbyNodes))
			cm.server.logger.Printf("Promoting %d standby nodes to active", numToPromote)

			for i := 0; i < numToPromote; i++ {
				node := standbyNodes[i]
				cm.server.registry.mu.Lock()
				node.Status = "active"
				node.Rank = len(activeNodes) + i
				cm.server.registry.mu.Unlock()

				activeNodes = append(activeNodes, node)
			}

			cm.server.metrics.mu.Lock()
			cm.server.metrics.metrics.activeNodes.Add(int32(numToPromote))
			cm.server.metrics.metrics.standbyNodes.Add(int32(-numToPromote))
			cm.server.metrics.mu.Unlock()
		}

		if len(activeNodes) < cm.server.config.ActiveNodes {
			cm.server.logger.Printf("Fatal: Insufficient nodes to continue operation. "+
				"Have %d active (including promoted standby), need %d",
				len(activeNodes), cm.server.config.ActiveNodes)
			// This will trigger server shutdown
			_ = cm.server.state.Transition(Failed)
			// This line won't be reached due to os.Exit in the state transition
			return fmt.Errorf("insufficient nodes")
		}

		// Sort nodes by rank to ensure consistent ordering
		sort.Slice(activeNodes, func(i, j int) bool {
			return activeNodes[i].Rank < activeNodes[j].Rank
		})

		if err := cm.notifyNodesInParallel(activeNodes); err != nil {
			cm.server.logger.Printf("Failed to notify nodes: %v", err)
			continue
		}

		cm.server.logger.Printf("Reconfiguration completed successfully with %d active nodes (%d promoted from standby)",
			len(activeNodes), min(neededNodes, len(standbyNodes)))

		if cm.server.state.Current() == Reconfiguring {
			if err := cm.server.state.Transition(Running); err != nil {
				cm.server.logger.Printf("Error transitioning back to running state: %v", err)
			}
		}

		return nil
	}

	// If all attempts failed, transition to Failed state which will exit the server
	cm.server.logger.Printf("Fatal: Failed to reconfigure cluster after %d attempts", maxAttempts)
	_ = cm.server.state.Transition(Failed)
	// This line won't be reached due to os.Exit in the state transition
	return fmt.Errorf("reconfiguration failed")
}

func (cm *ClusterManager) promoteStandbyNode() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Find available standby node
	var standbyNodeID string
	for nodeID, node := range cm.server.registry.nodes {
		if node.Status == "standby" {
			standbyNodeID = nodeID
			break
		}
	}

	if standbyNodeID == "" {
		return fmt.Errorf("no standby nodes available")
	}

	// Find lowest available rank
	usedRanks := make(map[int]bool)
	for _, node := range cm.server.registry.nodes {
		if node.Status == "active" {
			usedRanks[node.Rank] = true
		}
	}

	newRank := 0
	for usedRanks[newRank] {
		newRank++
	}

	// Update standby node
	node := cm.server.registry.nodes[standbyNodeID]
	node.Status = "active"
	node.Rank = newRank

	return nil
}

func (cm *ClusterManager) notifyNodesOfConfig(activeNodes []*NodeState) error {
	var wg sync.WaitGroup
	errors := make(chan error, len(activeNodes))

	for _, node := range activeNodes {
		if node.Status == "failed" {
			cm.server.logger.Printf("Skipping failed node %s during reconfiguration", node.NodeURL)
			continue
		}

		wg.Add(1)
		go func(node *NodeState) {
			defer wg.Done()
			if err := cm.notifyNodeOfConfig(node); err != nil {
				errors <- fmt.Errorf("failed to notify node %s: %v", node.NodeURL, err)
			}
		}(node)
	}

	wg.Wait()
	close(errors)

	// Collect any errors
	var errs []error
	for err := range errors {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		// only return error if we couldn't notify ANY valid nodes
		if len(errs) >= len(activeNodes) {
			return fmt.Errorf("failed to notify all nodes: %v", errs)
		}
		cm.server.logger.Printf("Warning: some nodes failed during reconfiguration: %v", errs)
	}

	return nil
}

func (cm *ClusterManager) removeNode(nodeID string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	node, exists := cm.server.registry.nodes[nodeID]
	if !exists {
		return fmt.Errorf("node %s not found", nodeID)
	}

	if err := cm.server.state.Transition(Reconfiguring); err != nil {
		return fmt.Errorf("failed to transition to reconfiguring state: %w", err)
	}

	// Update metrics based on node status
	if node.Status == "active" {
		cm.server.metrics.metrics.activeNodes.Add(-1)
	} else if node.Status == "standby" {
		cm.server.metrics.metrics.standbyNodes.Add(-1)
	}

	// Remove node from registry
	delete(cm.server.registry.nodes, nodeID)
	delete(cm.server.registry.health, nodeID)

	// Trigger cluster reconfiguration if needed
	if node.Status == "active" {
		return cm.reconfigureCluster()
	}

	return nil
}

func (cm *ClusterManager) notifyNodeOfConfig(node *NodeState) error {
	client := &http.Client{Timeout: 10 * time.Second}

	command := map[string]interface{}{
		"command": "restart",
		"env_vars": map[string]string{
			"RANK":        fmt.Sprintf("%d", node.Rank),
			"WORLD_SIZE":  fmt.Sprintf("%d", cm.server.config.ActiveNodes),
			"MASTER_ADDR": os.Getenv("MASTER_ADDR"),
			"MASTER_PORT": os.Getenv("MASTER_PORT"),
		},
	}

	payload, err := json.Marshal(command)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	resp, err := client.Post(
		fmt.Sprintf("%s/command", node.NodeURL),
		"application/json",
		bytes.NewBuffer(payload),
	)
	if err != nil {
		return fmt.Errorf("failed to send command to node: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("node returned error status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (cm *ClusterManager) checkAndStartProgram() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	println("checkAndStartProgram")

	currentState := cm.server.state.Current()
	if currentState != Initializing && currentState != Running {
		return fmt.Errorf("cannot start programs: server in %s state", currentState)
	}

	// Count active nodes
	var activeNodes []*NodeState
	for _, node := range cm.server.registry.nodes {
		if node.Status == "active" {
			activeNodes = append(activeNodes, node)
		}
	}

	expectedActive := cm.server.config.ExpectedNodes - cm.server.config.StandbyNodes

	println("activeNodes: ", len(activeNodes))

	// Check if we have all required active nodes
	if len(activeNodes) < expectedActive {
		cm.server.logger.Printf("Waiting for more nodes: have %d active, need %d",
			len(activeNodes), expectedActive)
		return nil // Not enough nodes yet
	}

	// Check if program is already running
	if cm.server.state.Current() == Running {
		return nil
	}

	cm.server.logger.Printf("All required nodes connected (%d active nodes). Starting program...",
		len(activeNodes))

	// Sort by node ID for consistent rank assignment
	sort.Slice(activeNodes, func(i, j int) bool {
		return activeNodes[i].NodeURL < activeNodes[j].NodeURL
	})

	// Assign ranks
	for i, node := range activeNodes {
		node.Rank = i
	}

	time.Sleep(3 * time.Second)

	// Start program on all active nodes
	if err := cm.startProgramOnNodes(activeNodes); err != nil {
		return fmt.Errorf("failed to start program: %w", err)
	}

	//// Update server state to running
	if err := cm.server.state.Transition(Running); err != nil {
		return fmt.Errorf("failed to transition server state: %w", err)
	}

	return nil
}

func (cm *ClusterManager) startProgramOnNodes(nodes []*NodeState) error {
	var wg sync.WaitGroup
	errors := make(chan error, len(nodes))

	cm.server.logger.Printf("Starting program on %d nodes...", len(nodes))

	for _, node := range nodes {
		wg.Add(1)
		go func(node *NodeState) {
			defer wg.Done()

			// Prepare environment variables
			envVars := map[string]string{
				"RANK":        fmt.Sprintf("%d", node.Rank),
				"WORLD_SIZE":  fmt.Sprintf("%d", len(nodes)),
				"MASTER_ADDR": cm.server.config.MasterAddr,
				"MASTER_PORT": cm.server.config.MasterPort,
			}

			cm.server.logger.Printf("Starting node %s with rank %d. Master address: %s:%s", node.NodeURL, node.Rank, cm.server.config.MasterAddr, cm.server.config.MasterPort)

			// Send start command
			if err := cm.sendStartCommand(node, envVars); err != nil {
				errors <- fmt.Errorf("failed to start node %s: %v", node.NodeURL, err)
			}
		}(node)
	}

	wg.Wait()
	close(errors)

	// Collect any errors
	var errs []error
	for err := range errors {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to start program on all nodes: %v", errs)
	}

	cm.server.logger.Printf("Successfully started program on all nodes")
	return nil
}

func (cm *ClusterManager) sendStartCommand(node *NodeState, envVars map[string]string) error {
	command := map[string]interface{}{
		"command":  "start",
		"env_vars": envVars,
	}

	payload, err := json.Marshal(command)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	println("node.NodeURL: ", node.NodeURL)

	resp, err := http.Post(
		fmt.Sprintf("%s/command", node.NodeURL),
		"application/json",
		bytes.NewBuffer(payload),
	)
	if err != nil {
		return fmt.Errorf("failed to send command to node: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("node returned error status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (cm *ClusterManager) notifyNodesInParallel(nodes []*NodeState) error {
	// First assign ranks
	for i, node := range nodes {
		node.Rank = i
	}

	var wg sync.WaitGroup
	type notificationResult struct {
		nodeURL string
		err     error
	}
	results := make(chan notificationResult, len(nodes))

	// Send notifications in parallel
	for _, node := range nodes {
		wg.Add(1)
		go func(node *NodeState) {
			defer wg.Done()
			err := cm.notifyNodeOfConfig(node)
			results <- notificationResult{
				nodeURL: node.NodeURL,
				err:     err,
			}
		}(node)
	}

	// Wait for all notifications in a separate goroutine
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results and handle failures
	failedNodes := make(map[string]struct{})
	var notificationErrors []error

	for result := range results {
		if result.err != nil {
			failedNodes[result.nodeURL] = struct{}{}
			notificationErrors = append(notificationErrors,
				fmt.Errorf("failed to notify node %s: %v", result.nodeURL, result.err))
		}
	}

	// If any nodes failed, mark them and trigger a retry
	if len(failedNodes) > 0 {
		cm.mu.Lock()
		for nodeURL := range failedNodes {
			if node, exists := cm.server.registry.nodes[nodeURL]; exists {
				oldStatus := node.Status
				node.Status = "failed"
				if oldStatus == "active" {
					cm.server.metrics.metrics.activeNodes.Add(-1)
					cm.server.metrics.metrics.failedNodes.Add(1)
				}
				cm.server.logger.Printf("Marked node %s as failed due to notification failure", nodeURL)
			}
		}
		cm.mu.Unlock()

		// Return error to trigger retry with new node set
		return fmt.Errorf("some nodes failed notification, marked %d nodes as failed", len(failedNodes))
	}

	return nil
}

func (cm *ClusterManager) getUsableNodes() ([]*NodeState, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	var usableNodes []*NodeState
	now := time.Now()

	for nodeID, node := range cm.server.registry.nodes {
		if node.Status != "active" {
			continue
		}

		health, exists := cm.server.registry.health[nodeID]
		if !exists {
			continue
		}

		// Check if node is responsive
		if now.Sub(health.LastHeartbeat) <= cm.server.config.HeartbeatTimeout {
			nodeCopy := *node
			usableNodes = append(usableNodes, &nodeCopy)
		} else {
			// Mark unresponsive node as failed
			cm.server.logger.Printf("Marking node %s as failed due to heartbeat timeout", nodeID)
			node.Status = "failed"
			health.Status = "failed"
			cm.server.metrics.metrics.activeNodes.Add(-1)
			cm.server.metrics.metrics.failedNodes.Add(1)
		}
	}

	return usableNodes, nil
}
