package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

func (m *Monitor) handleLogCCL(c *gin.Context) {
	var log struct {
		OpType      string `json:"opType"`
		ContextRank int64  `json:"context_rank"`
		RemoteRank  int64  `json:"remoteRank"`
		StartTime   int64  `json:"startTime"`
		EndTime     int64  `json:"endTime"`
		Finished    bool   `json:"finished"`
	}

	if err := c.ShouldBindJSON(&log); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid data"})
		return
	}

	if log.Finished && (log.OpType == "send" || log.OpType == "recv") {
		latency := float64(log.EndTime-log.StartTime) / 1000000
		m.updateLatencyWindow(int(log.RemoteRank), latency)
	}

	c.JSON(http.StatusOK, gin.H{"message": "Processed"})
}

func (m *Monitor) handleRestart(c *gin.Context) {
	if m.getState() == Failed {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Node in failed state"})
		return
	}

	var config RestartConfig
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid config"})
		return
	}

	m.setState(Restarting)

	// Update configuration
	m.rank = config.NewRank
	m.worldSize = config.WorldSize

	// Prepare environment variables
	envVars := map[string]string{
		"RANK":        fmt.Sprintf("%d", config.NewRank),
		"WORLD_SIZE":  fmt.Sprintf("%d", config.WorldSize),
		"MASTER_ADDR": os.Getenv("MASTER_ADDR"),
		"MASTER_PORT": os.Getenv("MASTER_PORT"),
	}

	// Restart agent process
	if err := m.restartAgent(envVars); err != nil {
		m.setState(Failed)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to restart: %v", err)})
		return
	}

	m.setState(Running)
	c.JSON(http.StatusOK, gin.H{"message": "Restart successful"})
}

func (m *Monitor) handleMetrics(c *gin.Context) {
	currentState := m.getState()

	m.latencyMutex.RLock()
	avgLatencies := make(map[int]float64)
	for rank, window := range m.latencyWindow {
		if len(window) > 0 {
			var sum float64
			for _, l := range window {
				sum += l
			}
			avgLatencies[rank] = sum / float64(len(window))
		}
	}
	m.latencyMutex.RUnlock()

	c.JSON(http.StatusOK, gin.H{
		"state":      currentState.String(),
		"rank":       m.rank,
		"world_size": m.worldSize,
		"ram_usage":  m.ramUsage,
		"cpu_usage":  m.cpuUsage,
		"latencies":  avgLatencies,
	})
}

func (m *Monitor) handleActivate(c *gin.Context) {
	currentState := m.getState()
	if currentState != Standby {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Node not in standby state"})
		return
	}

	if err := m.startAgent(m.getEnvVars()); err != nil {
		m.setState(Failed)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start agent"})
		return
	}

	m.setState(Running)
	c.JSON(http.StatusOK, gin.H{"message": "Node activated"})
}

func (m *Monitor) handleRemove(c *gin.Context) {
	currentState := m.getState()
	if currentState == Failed || currentState == Standby {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid state for removal"})
		return
	}

	if err := m.stopAgent(); err != nil {
		fmt.Printf("Warning: failed to stop agent: %v\n", err)
	}

	m.setState(Standby)
	c.JSON(http.StatusOK, gin.H{"message": "Node removed"})
}
