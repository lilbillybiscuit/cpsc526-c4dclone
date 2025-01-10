package src

import (
	"time"
)

// HealthMonitor manages node health checking
type HealthMonitor struct {
	server   *C4DServer
	registry *NodeRegistry
	config   *ServerConfig
	metrics  *ServerMetrics
	stopChan chan struct{}
}

func NewHealthMonitor(server *C4DServer, registry *NodeRegistry, config *ServerConfig, metrics *ServerMetrics) *HealthMonitor {
	return &HealthMonitor{
		server:   server,
		registry: registry,
		config:   config,
		metrics:  metrics,
		stopChan: make(chan struct{}),
	}
}

func (hm *HealthMonitor) Start() {
	ticker := time.NewTicker(hm.config.HeartbeatTimeout / 2)
	defer ticker.Stop()

	for {
		select {
		case <-hm.stopChan:
			return
		case <-ticker.C:
			hm.checkNodesHealth()
		}
	}
}

func (hm *HealthMonitor) checkNodesHealth() {
	now := time.Now()

	// Check node health
	hm.registry.mu.RLock()
	for nodeID, health := range hm.registry.health {
		if health.Status != "failed" &&
			now.Sub(health.LastHeartbeat) > hm.config.HeartbeatTimeout {
			hm.registry.mu.RUnlock()
			hm.handleNodeTimeout(nodeID)
			hm.registry.mu.RLock()
		}
	}
	hm.registry.mu.RUnlock()

	hm.server.sessionManager.CleanupSessions(hm.config.HeartbeatTimeout * 2)
}

func (hm *HealthMonitor) handleNodeTimeout(nodeID string) {
	hm.registry.mu.Lock()
	defer hm.registry.mu.Unlock()

	if node, exists := hm.registry.nodes[nodeID]; exists {
		node.Status = "failed"
		hm.metrics.failedNodes.Add(1)
		if node.Status == "active" {
			hm.metrics.activeNodes.Add(-1)
		} else if node.Status == "standby" {
			hm.metrics.standbyNodes.Add(-1)
		}
	}
}

func (hm *HealthMonitor) Stop() {
	close(hm.stopChan)
}
