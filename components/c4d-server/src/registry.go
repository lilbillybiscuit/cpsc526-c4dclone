package src

import (
	"fmt"
	"time"
)

type NodeRegistry struct {
	nodes  map[string]*NodeState
	health map[string]*NodeHealth
	mu     MonitoredRWMutex
}

func NewNodeRegistry() *NodeRegistry {
	return &NodeRegistry{
		nodes:  make(map[string]*NodeState),
		health: make(map[string]*NodeHealth),
	}
}

func (r *NodeRegistry) RegisterNode(nodeID string, nodeURL string, status string, rank int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	//if _, exists := r.nodes[nodeID]; exists {
	//	return fmt.Errorf("node %s already registered", nodeID)
	//}

	r.nodes[nodeID] = &NodeState{
		NodeURL: nodeURL,
		Status:  status,
		Rank:    rank,
	}

	r.health[nodeID] = &NodeHealth{
		LastHeartbeat: time.Now(),
		Status:        status,
		FailureCount:  0,
	}
}

func (r *NodeRegistry) UpdateNodeHealth(nodeID string, health *NodeHealth) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.nodes[nodeID]; !exists {
		return fmt.Errorf("node %s not found", nodeID)
	}

	r.health[nodeID] = health
	return nil
}

func (r *NodeRegistry) GetNode(nodeID string) (*NodeState, *NodeHealth, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	node, exists := r.nodes[nodeID]
	if !exists {
		return nil, nil, fmt.Errorf("node %s not found", nodeID)
	}

	health := r.health[nodeID]
	return node, health, nil
}

func (r *NodeRegistry) MarkNodeFailed(nodeID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	node, exists := r.nodes[nodeID]
	if !exists {
		return fmt.Errorf("node %s not found", nodeID)
	}

	node.Status = "failed"
	r.health[nodeID].Status = "failed"
	r.health[nodeID].FailureCount++
	r.health[nodeID].LastError = fmt.Errorf("node marked as failed at %v", time.Now())

	return nil
}

func (r *NodeRegistry) CountUsableNodes() (active int, standby int) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	now := time.Now()
	for nodeID, node := range r.nodes {
		health, exists := r.health[nodeID]
		if !exists {
			continue
		}
		if node.Status == "failed" {
			continue
		}
		if now.Sub(health.LastHeartbeat) > DefaultHeartbeatTimeout {
			continue
		}

		if node.Status == "active" {
			active++
		} else if node.Status == "standby" {
			standby++
		}
	}
	return
}
