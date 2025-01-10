package src

import (
	"sync/atomic"
	"time"
)

// State represents the server's operational state
type State int

const (
	Initializing State = iota
	Running
	Reconfiguring
	ShuttingDown
	Failed
)

func (s State) String() string {
	switch s {
	case Initializing:
		return "Initializing"
	case Running:
		return "Running"
	case Reconfiguring:
		return "Reconfiguring"
	case ShuttingDown:
		return "ShuttingDown"
	case Failed:
		return "Failed"
	default:
		return "Unknown"
	}
}

// ServerConfig holds the server's configuration parameters
type ServerConfig struct {
	ExpectedNodes    int
	StandbyNodes     int
	ActiveNodes      int
	HeartbeatTimeout time.Duration
	LatencyThreshold time.Duration
	GRPCPort         string
	APIPort          string

	MasterAddr string
	MasterPort string
}

// NodeState represents the current state of a node
type NodeState struct {
	NodeURL string
	Status  string // "active", "standby", "failed"
	Rank    int
	Version int64
}

// NodeHealth tracks the health metrics of a node
type NodeHealth struct {
	LastHeartbeat time.Time
	FailureCount  int
	Status        string
	ProcessID     int64
	Latencies     []float64
	CPUUsage      float64
	MemoryUsage   float64
	LastError     error
}

// ServerMetrics tracks server-wide metrics
type ServerMetrics struct {
	activeNodes      atomic.Int32
	standbyNodes     atomic.Int32
	failedNodes      atomic.Int32
	totalTransitions atomic.Int64
	lastError        atomic.Value
	mu               MonitoredRWMutex
}

type ClusterState struct {
	MasterAddr   string
	MasterPort   string
	ActiveNodes  int32
	StandbyNodes int32
	ReadyToStart bool
	mu           MonitoredRWMutex
}
