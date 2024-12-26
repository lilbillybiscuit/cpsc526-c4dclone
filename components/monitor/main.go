package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

	"monitor/proto"
)

const (
	serverBaseURL           = "localhost:8091"
	windowSize              = 100 // Size of latency sliding window
	completionCheckInterval = 5 * time.Second
)

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
type Monitor struct {
	// Essential state
	nodeID    string
	nodeURL   string
	rank      int
	worldSize int
	isStandby bool

	// Metrics tracking
	latencyWindow map[int][]float64 // remoteRank -> latency window
	cpuUsage      float64
	ramUsage      float64

	// Synchronization
	stopChan     chan struct{}
	latencyMutex sync.RWMutex
	rankMutex    sync.RWMutex

	// Communication
	grpcClient      c4d.C4DServiceClient
	isCompleted     bool
	completionMutex sync.RWMutex
}

func (m *Monitor) checkCompletion() error {
	var status AgentStatus
	if err := m.callAgentEndpoint("GET", "/status", nil, &status); err != nil {
		return fmt.Errorf("failed to check completion status: %w", err)
	}

	// Check if process has completed successfully
	if status.LastExecution.ExitCode != nil && *status.LastExecution.ExitCode == 0 {
		m.completionMutex.Lock()
		m.isCompleted = true
		m.completionMutex.Unlock()

		// Notify server of successful completion
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		req := &c4d.MetricsRequest{
			NodeId: m.nodeID,
			Status: &c4d.NodeStatus{
				IsAlive:               false,
				ProcessId:             status.LastExecution.PID,
				ExitCode:              int32(*status.LastExecution.ExitCode),
				CompletedSuccessfully: true,
			},
		}

		_, err := m.grpcClient.SendMetrics(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to send completion status: %w", err)
		}
	}

	return nil
}

func NewMonitor() (*Monitor, error) {
	taskID := os.Getenv("TASK_ID")
	namespace := os.Getenv("NAMESPACE")
	port := os.Getenv("PORT")
	// if port not set, default to 8081
	if port == "" {
		port = "8081"
	}
	nodeURL := fmt.Sprintf("http://%s.%s:%s", taskID, namespace, port)

	var conn *grpc.ClientConn

	for {
		var err error
		conn, err = grpc.NewClient(serverBaseURL,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithKeepaliveParams(keepalive.ClientParameters{
				Time:                10 * time.Second,
				Timeout:             5 * time.Second,
				PermitWithoutStream: true,
			}))
		if err != nil {
			return nil, fmt.Errorf("failed to connect to server: %v", err)
		} else if conn != nil {
			break
		}
	}

	rank, _ := strconv.Atoi(os.Getenv("RANK"))
	worldSize, _ := strconv.Atoi(os.Getenv("WORLD_SIZE"))

	return &Monitor{
		nodeID:        taskID + "." + port,
		nodeURL:       nodeURL,
		rank:          rank,
		worldSize:     worldSize,
		isStandby:     false,
		latencyWindow: make(map[int][]float64),
		stopChan:      make(chan struct{}),
		grpcClient:    c4d.NewC4DServiceClient(conn),
		isCompleted:   false,
	}, nil
}

// -- Agent Methods

func (m *Monitor) updateLatencyWindow(remoteRank int, latency float64) float64 {
	m.latencyMutex.Lock()
	defer m.latencyMutex.Unlock()

	if _, exists := m.latencyWindow[remoteRank]; !exists {
		m.latencyWindow[remoteRank] = make([]float64, 0, windowSize)
	}

	window := m.latencyWindow[remoteRank]
	window = append(window, latency)
	if len(window) > windowSize {
		window = window[1:]
	}
	m.latencyWindow[remoteRank] = window

	// Calculate average
	var sum float64
	for _, l := range window {
		sum += l
	}
	return sum / float64(len(window))
}

func (m *Monitor) startAgent(envVars map[string]string) error {
	payload := map[string]interface{}{
		"env_vars": envVars,
	}

	var agentStatus AgentStatus
	if err := m.callAgentEndpoint("POST", "/start", payload, &agentStatus); err != nil {
		return fmt.Errorf("failed to start agent: %w", err)
	}

	fmt.Printf("Agent started with PID %d\n", agentStatus.CurrentPID)
	return nil
}

func (m *Monitor) stopAgent() error {
	var agentStatus AgentStatus
	if err := m.callAgentEndpoint("POST", "/stop", nil, &agentStatus); err != nil {
		return fmt.Errorf("failed to stop agent: %w", err)
	}

	fmt.Printf("Agent stopped. Last PID: %d\n", agentStatus.LastExecution.PID)
	return nil
}

func (m *Monitor) restartAgent(envVars map[string]string) error {
	payload := map[string]interface{}{
		"env_vars": envVars,
	}

	var agentStatus AgentStatus
	if err := m.callAgentEndpoint("POST", "/restart", payload, &agentStatus); err != nil {
		return fmt.Errorf("failed to restart agent: %w", err)
	}

	fmt.Printf("Agent restarted with PID %d\n", agentStatus.CurrentPID)
	return nil
}

func (m *Monitor) fetchAgentMetrics() error {
	var metrics AgentMetrics
	if err := m.callAgentEndpoint("GET", "/metrics", nil, &metrics); err != nil {
		return fmt.Errorf("failed to fetch metrics: %w", err)
	}

	if metrics.Metrics.ProcessMetrics != nil {
		m.cpuUsage = metrics.Metrics.ProcessMetrics.CPUUsage
		m.ramUsage = float64(metrics.Metrics.ProcessMetrics.MemoryBytes)
	}

	return nil
}

func (m *Monitor) fetchAgentStatus() error {
	var status AgentStatus
	if err := m.callAgentEndpoint("GET", "/status", nil, &status); err != nil {
		return fmt.Errorf("failed to fetch status: %w", err)
	}

	return nil
}

func (m *Monitor) callAgentEndpoint(method, endpoint string, payload interface{}, result interface{}) error {
	url := fmt.Sprintf("http://localhost:8080%s", endpoint)

	var body io.Reader
	if payload != nil {
		jsonData, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %w", err)
		}
		body = bytes.NewReader(jsonData)
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("agent returned error status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

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

func (m *Monitor) sendMetrics() {
	metricsTicker := time.NewTicker(time.Second)
	completionTicker := time.NewTicker(completionCheckInterval)
	defer metricsTicker.Stop()
	defer completionTicker.Stop()

	for {
		select {
		case <-m.stopChan:
			return
		case <-metricsTicker.C:
			m.completionMutex.RLock()
			if !m.isCompleted {
				m.sendMetricsToServer()
			}
			m.completionMutex.RUnlock()
		case <-completionTicker.C:
			if err := m.checkCompletion(); err != nil {
				fmt.Printf("Failed to check completion status: %v\n", err)
			}

			// If completed, stop sending metrics
			m.completionMutex.RLock()
			if m.isCompleted {
				m.completionMutex.RUnlock()
				return
			}
			m.completionMutex.RUnlock()
		}
	}
}

func (m *Monitor) sendMetricsToServer() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Fetch current agent status
	var agentStatus AgentStatus
	if err := m.callAgentEndpoint("GET", "/status", nil, &agentStatus); err != nil {
		fmt.Printf("Failed to fetch agent status: %v\n", err)
		return
	}

	m.latencyMutex.RLock()
	avgLatencies := make(map[int32]float64)
	for remoteRank, window := range m.latencyWindow {
		if len(window) > 0 {
			var sum float64
			for _, l := range window {
				sum += l
			}
			avg := sum / float64(len(window))
			avgLatencies[int32(remoteRank)] = avg
		}
	}
	m.latencyMutex.RUnlock()

	req := &c4d.MetricsRequest{
		NodeId: m.nodeID,
		Metrics: &c4d.Metrics{
			RamUsage: m.ramUsage,
			CpuUsage: m.cpuUsage,
			RankLatencies: &c4d.RankLatencies{
				Rank:         int32(m.rank),
				AvgLatencies: avgLatencies,
			},
		},
		Status: &c4d.NodeStatus{
			IsAlive:   true,
			ProcessId: agentStatus.CurrentPID,
		},
	}

	// Include exit information if available
	if agentStatus.LastExecution.ExitCode != nil {
		req.Status.ExitCode = int32(*agentStatus.LastExecution.ExitCode)
		req.Status.CompletedSuccessfully = *agentStatus.LastExecution.ExitCode == 0
	}

	_, err := m.grpcClient.SendMetrics(ctx, req)
	if err != nil {
		fmt.Printf("Failed to send metrics: %v\n", err)
	}
}

type RestartConfig struct {
	NewRank     int                      `json:"new_rank"`
	WorldSize   int                      `json:"world_size"`
	ActiveNodes []map[string]interface{} `json:"active_nodes"`
}

func (m *Monitor) handleRestart(c *gin.Context) {
	var config RestartConfig
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid config"})
		return
	}

	m.rankMutex.Lock()
	m.rank = config.NewRank
	m.worldSize = config.WorldSize
	m.rankMutex.Unlock()

	// Clear latency data
	m.latencyMutex.Lock()
	m.latencyWindow = make(map[int][]float64)
	m.latencyMutex.Unlock()

	// Prepare new environment variables
	envVars := map[string]string{
		"RANK":        fmt.Sprintf("%d", config.NewRank),
		"WORLD_SIZE":  fmt.Sprintf("%d", config.WorldSize),
		"MASTER_ADDR": os.Getenv("MASTER_ADDR"),
		"MASTER_PORT": os.Getenv("MASTER_PORT"),
	}

	// Stop current process
	if err := m.stopAgent(); err != nil {
		fmt.Printf("Warning: failed to stop agent: %v\n", err)
	}

	// Start with new configuration
	if err := m.startAgent(envVars); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to restart agent: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Configuration updated and agent restarted",
		"rank":    config.NewRank,
	})
}

func (m *Monitor) handleMetrics(c *gin.Context) {
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

	m.completionMutex.RLock()
	isCompleted := m.isCompleted
	m.completionMutex.RUnlock()

	c.JSON(http.StatusOK, gin.H{
		"rank":       m.rank,
		"world_size": m.worldSize,
		"ram_usage":  m.ramUsage,
		"cpu_usage":  m.cpuUsage,
		"latencies":  avgLatencies,
		"completed":  isCompleted,
	})
}

func (m *Monitor) handleActivate(c *gin.Context) {
	m.rankMutex.Lock()
	wasStandby := m.isStandby
	m.isStandby = false
	m.rankMutex.Unlock()

	if wasStandby {

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_, err := m.grpcClient.Register(ctx, &c4d.RegisterRequest{
			NodeId:     m.nodeID,
			NodeUrl:    m.nodeURL,
			NodeStatus: "active",
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to register as active"})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "Node activated"})
}

func (m *Monitor) handleRemove(c *gin.Context) {
	m.rankMutex.Lock()
	m.isStandby = true
	m.rankMutex.Unlock()

	m.latencyMutex.Lock()
	m.latencyWindow = make(map[int][]float64)
	m.latencyMutex.Unlock()

	close(m.stopChan)
	c.JSON(http.StatusOK, gin.H{"message": "Node removed"})
}

func (m *Monitor) Start() error {

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	nodeStatus := "active"
	if m.isStandby {
		nodeStatus = "standby"
	}
	_, err := m.grpcClient.Register(ctx, &c4d.RegisterRequest{
		NodeId:     m.nodeID,
		NodeUrl:    m.nodeURL,
		NodeStatus: nodeStatus,
	})

	if err != nil {
		return fmt.Errorf("failed to register: %v", err)
	}

	if !m.isStandby {
		go m.sendMetrics()
	}

	return nil
}

func (m *Monitor) Stop() {
	close(m.stopChan)
}

func main() {
	monitor, err := NewMonitor()
	if err != nil {
		fmt.Printf("Failed to create monitor: %v\n", err)
		os.Exit(1)
	}

	if err := monitor.Start(); err != nil {
		fmt.Printf("Failed to start monitor: %v\n", err)
		os.Exit(1)
	}

	r := gin.Default()
	r.POST("/log_ccl", monitor.handleLogCCL)
	r.POST("/restart", monitor.handleRestart)
	r.POST("/activate", monitor.handleActivate)
	r.POST("/remove", monitor.handleRemove)
	r.GET("/metrics", monitor.handleMetrics)

	port := os.Getenv("PORT")
	// if port not set, default to 8081
	if port == "" {
		port = "8081"
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		<-sigCh
		monitor.Stop()
		os.Exit(0)
	}()

	if err := r.Run(":" + port); err != nil {
		fmt.Printf("Server failed: %v\n", err)
		os.Exit(1)
	}
}
