package main

import (
	"errors"
	c4d "monitor/proto"
	"sync"
	"time"
)

// State definitions
type State int

const (
	Initializing State = iota
	Stopped
	Running
	Standby
	Restarting
	Failed
)

// Error definitions
var (
	ErrInvalidState = errors.New("invalid state for operation")
	ErrAgentFailed  = errors.New("agent process failed")
)

// Agent-related types
type AgentStatus struct {
	CurrentStatus string `json:"current_status"`
	CurrentPID    int64  `json:"current_pid"`
	LastExecution struct {
		StartTime  int64             `json:"start_time"`
		EndTime    *int64            `json:"end_time"`
		ExitCode   *int              `json:"exit_code"`
		ExitSignal *string           `json:"exit_signal"`
		PID        int64             `json:"pid"`
		EnvVars    map[string]string `json:"env_vars"`
		ScriptPath string            `json:"script_path"`
	} `json:"last_execution"`
}

type AgentMetrics struct {
	Timestamp int64 `json:"timestamp"`
	Metrics   struct {
		SystemCPUUsage             float64 `json:"system_cpu_usage"`
		SystemMemoryTotalBytes     int64   `json:"system_memory_total_bytes"`
		SystemMemoryUsedBytes      int64   `json:"system_memory_used_bytes"`
		SystemMemoryFreeBytes      int64   `json:"system_memory_free_bytes"`
		SystemMemoryAvailableBytes int64   `json:"system_memory_available_bytes"`
		ProcessMetrics             *struct {
			PID         int64   `json:"pid"`
			CPUUsage    float64 `json:"cpu_usage"`
			MemoryBytes int64   `json:"memory_bytes"`
			RunTimeSecs int64   `json:"run_time_secs"`
			Status      string  `json:"status"`
		} `json:"process_metrics"`
	} `json:"metrics"`
}

type RestartConfig struct {
	NewRank     int                      `json:"new_rank"`
	WorldSize   int                      `json:"world_size"`
	ActiveNodes []map[string]interface{} `json:"active_nodes"`
}

type Monitor struct {
	// Essential state
	nodeID    string
	nodeURL   string
	rank      int
	worldSize int

	state    State
	stateMux sync.RWMutex

	// Metrics tracking
	latencyWindow map[int][]float64 // remoteRank -> latency window
	cpuUsage      float64
	ramUsage      float64

	// Synchronization
	stopChan     chan struct{}
	latencyMutex sync.RWMutex

	// Communication
	grpcClient c4d.C4DServiceClient

	heartbeatTicker *time.Ticker
	heartbeatDone   chan struct{}
}

// State string conversion
func (s State) String() string {
	switch s {
	case Initializing:
		return "Initializing"
	case Running:
		return "Running"
	case Standby:
		return "Standby"
	case Restarting:
		return "Restarting"
	case Failed:
		return "Failed"
	default:
		return "Unknown"
	}
}
