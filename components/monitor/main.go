package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"google.golang.org/grpc/keepalive"
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

	"monitor/proto" // Update with your module name
)

// Constants for agent communication
const (
	agentBaseURL  = "http://localhost:8080"
	serverBaseURL = "localhost:8091"
)

type CCLLog struct {
	OpType      string  `json:"opType"`
	Algorithm   string  `json:"algorithm,omitempty"`
	DataType    string  `json:"dataType,omitempty"`
	Count       int64   `json:"count,omitempty"`
	RootRank    int64   `json:"rootRank,omitempty"`
	ContextRank int64   `json:"context_rank"`
	ContextSize int64   `json:"context_size"`
	StartTime   int64   `json:"startTime"`
	EndTime     int64   `json:"endTime"`
	Filename    string  `json:"filename"`
	Finished    bool    `json:"finished"`
	RemoteRank  int64   `json:"remoteRank,omitempty"`
	Bytes       int64   `json:"bytes,omitempty"`
	Timestamp   float64 `json:"timestamp"`
}

type LatencyStats struct {
	AverageLatency float64
	LatencyCount   int64
	LastUpdate     time.Time
}

// Monitor struct to hold the state of the monitoring client
type Monitor struct {
	stopEvent           chan bool
	ramUsage            float64
	cpuUsage            float64
	computeEngineStatus string
	metrics             *c4d.Metrics
	c4dServerURL        string
	isStandby           bool
	nodeStatus          string
	distEnvVars         map[string]string
	cclLogs             []*c4d.CCLLogGroup
	cclLock             sync.Mutex
	nodeURL             string
	agentMetrics        *c4d.AgentMetrics
	agentStatus         *c4d.AgentStatus
	grpcClient          c4d.C4DServiceClient
	rank                int
	worldSize           int
	activeNodes         map[string]NodeInfo // nodeID -> NodeInfo
	rankMutex           sync.RWMutex

	//latencyStats  LatencyStats
	latencyWindow map[int][]float64 // Rolling window of recent latencies
	windowSize    int               // Size of rolling window for latency calculation
	latencyMutex  sync.RWMutex
}

type NodeInfo struct {
	Rank   int    `json:"rank"`
	URL    string `json:"url"`
	NodeID string `json:"node_id"`
}

type RestartConfig struct {
	NewRank     int                      `json:"new_rank"`
	WorldSize   int                      `json:"world_size"`
	ActiveNodes []map[string]interface{} `json:"active_nodes"`
}

type RankLatencies struct {
	Rank         int32
	AvgLatencies map[int32]float64 // remoteRank -> averaged latency
}

func NewMonitor() (*Monitor, error) {
	taskID := os.Getenv("TASK_ID")
	namespace := os.Getenv("NAMESPACE")
	nodeURL := fmt.Sprintf("http://%s.%s:8081", taskID, namespace)

	// Setup gRPC connection with modern options
	var conn *grpc.ClientConn
	var err error
	maxRetries := 3

	for retry := 0; retry < maxRetries; retry++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

		opts := []grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithBlock(),
			grpc.WithDefaultServiceConfig(`{
                "loadBalancingPolicy": "round_robin",
                "healthCheckConfig": {
                    "serviceName": ""
                }
            }`),
			grpc.WithKeepaliveParams(keepalive.ClientParameters{
				Time:                10 * time.Second,
				Timeout:             5 * time.Second,
				PermitWithoutStream: true,
			}),
		}

		conn, err = grpc.DialContext(ctx, serverBaseURL, opts...)
		cancel()

		if err != nil {
			if retry == maxRetries-1 {
				return nil, fmt.Errorf("failed to connect to gRPC server after %d retries: %v", maxRetries, err)
			}
			time.Sleep(time.Second * time.Duration(retry+1))
			continue
		}
		break
	}
	initialRank, err := strconv.Atoi(os.Getenv("RANK"))
	if err != nil {
		initialRank = -1 // Invalid rank until properly assigned
	}

	initialWorldSize, err := strconv.Atoi(os.Getenv("WORLD_SIZE"))
	if err != nil {
		initialWorldSize = 0 // Invalid world size until properly assigned
	}

	monitor := &Monitor{
		stopEvent:    make(chan bool),
		ramUsage:     0,
		cpuUsage:     0,
		metrics:      &c4d.Metrics{},
		c4dServerURL: serverBaseURL,
		isStandby:    false,
		nodeStatus:   "active",
		distEnvVars: map[string]string{
			"MASTER_ADDR": os.Getenv("MASTER_ADDR"),
			"MASTER_PORT": os.Getenv("MASTER_PORT"),
			"WORLD_SIZE":  os.Getenv("WORLD_SIZE"),
			"RANK":        os.Getenv("RANK"),
			"TASK_ID":     taskID,
		},
		cclLogs:       make([]*c4d.CCLLogGroup, 0),
		nodeURL:       nodeURL,
		agentMetrics:  &c4d.AgentMetrics{},
		agentStatus:   &c4d.AgentStatus{},
		grpcClient:    c4d.NewC4DServiceClient(conn),
		rank:          initialRank,
		worldSize:     initialWorldSize,
		activeNodes:   make(map[string]NodeInfo),
		latencyWindow: make(map[int][]float64),
		windowSize:    100,
	}

	return monitor, nil
}

// --- CCL Log Handling ---

func (m *Monitor) AppendCCLLog(log *c4d.CCLLogGroup) {
	m.cclLock.Lock()
	defer m.cclLock.Unlock()
	m.cclLogs = append(m.cclLogs, log)
}

func (m *Monitor) GetAndClearCCLLogs() []*c4d.CCLLogGroup {
	m.cclLock.Lock()
	defer m.cclLock.Unlock()
	logs := make([]*c4d.CCLLogGroup, len(m.cclLogs))
	copy(logs, m.cclLogs)
	m.cclLogs = []*c4d.CCLLogGroup{}
	return logs
}

// --- Agent Interaction ---

func (m *Monitor) callAgentEndpoint(endpoint string, method string, payload interface{}) (interface{}, error) {
	url := fmt.Sprintf("%s%s", agentBaseURL, endpoint)

	var reqBody io.Reader
	if payload != nil {
		jsonData, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal payload: %w", err)
		}
		reqBody = bytes.NewReader(jsonData)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:       100,
			IdleConnTimeout:    90 * time.Second,
			DisableCompression: true,
			ForceAttemptHTTP2:  true,
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call agent endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("agent responded with error: %s, body: %s", resp.Status, bodyBytes)
	}

	var result interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode agent response: %w", err)
	}

	return result, nil
}

func (m *Monitor) fetchAgentMetrics() error {
	// TODO: this is actually broken, need to fix
	rawMetrics, err := m.callAgentEndpoint("/metrics", "GET", nil)
	if err != nil {
		return fmt.Errorf("error fetching metrics from agent: %w", err)
	}

	metricsMap, ok := rawMetrics.(map[string]interface{})
	if !ok {
		return fmt.Errorf("unexpected format for agent metrics")
	}

	metricsJSON, err := json.Marshal(metricsMap)
	if err != nil {
		return fmt.Errorf("failed to marshal agent metrics to JSON: %w", err)
	}

	var agentMetrics c4d.AgentMetrics
	if err := json.Unmarshal(metricsJSON, &agentMetrics); err != nil {
		return fmt.Errorf("failed to unmarshal agent metrics from JSON: %w", err)
	}

	m.agentMetrics = &agentMetrics
	return nil
}

func (m *Monitor) fetchAgentStatus() error {
	rawStatus, err := m.callAgentEndpoint("/status", "GET", nil)
	if err != nil {
		return fmt.Errorf("error fetching status from agent: %w", err)
	}

	statusMap, ok := rawStatus.(map[string]interface{})
	if !ok {
		return fmt.Errorf("unexpected format for agent status")
	}

	statusJSON, err := json.Marshal(statusMap)
	if err != nil {
		return fmt.Errorf("failed to marshal agent status to JSON: %w", err)
	}

	var agentStatus c4d.AgentStatus
	if err := json.Unmarshal(statusJSON, &agentStatus); err != nil {
		return fmt.Errorf("failed to unmarshal agent status from JSON: %w", err)
	}

	m.agentStatus = &agentStatus
	return nil
}

func (m *Monitor) startAgent(envVars map[string]string, scriptPath string) error {
	payload := map[string]interface{}{"env_vars": envVars}
	_, err := m.callAgentEndpoint("/start", "POST", payload)
	return err
}

func (m *Monitor) stopAgent() error {
	_, err := m.callAgentEndpoint("/stop", "POST", nil)
	return err
}

func (m *Monitor) restartAgent(envVars map[string]string, scriptPath string) error {
	payload := map[string]interface{}{
		"env_vars":    envVars,
		"script_path": scriptPath,
	}
	_, err := m.callAgentEndpoint("/restart", "POST", payload)
	return err
}

// --- Metrics and Status ---

func (m *Monitor) UpdateMetrics() {
	for {
		select {
		case <-m.stopEvent:
			return
		default:
			if err := m.fetchAgentMetrics(); err != nil {
				fmt.Printf("Failed to fetch agent metrics: %v\n", err)
			}
			if err := m.fetchAgentStatus(); err != nil {
				fmt.Printf("Failed to fetch agent status: %v\n", err)
			}

			if m.agentMetrics != nil && m.agentMetrics.SystemMetrics != nil {
				m.ramUsage = float64(m.agentMetrics.SystemMetrics.SystemMemoryUsedBytes)
				m.cpuUsage = m.agentMetrics.SystemMetrics.SystemCpuUsage
			}

			if m.agentStatus != nil {
				m.computeEngineStatus = m.agentStatus.CurrentStatus
			}

			time.Sleep(1 * time.Second)
		}
	}
}

// --- Server Communication (gRPC) ---
func (m *Monitor) SendMetricsAndCCLLogsToServer() {
	for {
		select {
		case <-m.stopEvent:
			return
		default:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			m.rankMutex.RLock()
			currentRank := m.rank
			m.rankMutex.RUnlock()

			// Get averaged latencies
			m.latencyMutex.RLock()
			avgLatencies := make(map[int32]float64)
			for remoteRank, latencies := range m.latencyWindow {
				if len(latencies) > 0 {
					var sum float64
					for _, l := range latencies {
						sum += l
					}
					avgLatencies[int32(remoteRank)] = sum / float64(len(latencies))
				}
			}
			m.latencyMutex.RUnlock()

			// Only send essential metrics
			req := &c4d.MetricsRequest{
				NodeId: os.Getenv("TASK_ID"),
				Metrics: &c4d.Metrics{
					RamUsage: m.ramUsage,
					CpuUsage: m.cpuUsage,
					// Only send averaged latencies
					RankLatencies: &c4d.RankLatencies{
						Rank:         int32(currentRank),
						AvgLatencies: avgLatencies,
					},
				},
				// Only send critical status info
				Status: &c4d.NodeStatus{
					IsAlive:   m.agentStatus != nil && m.agentStatus.CurrentStatus == "running",
					ProcessId: m.agentStatus.CurrentPid,
				},
			}

			maxRetries := 3
			for retry := 0; retry < maxRetries; retry++ {
				resp, err := m.grpcClient.SendMetrics(ctx, req)
				if err != nil {
					if retry == maxRetries-1 {
						fmt.Printf("Failed to send metrics after %d retries: %v\n", maxRetries, err)
					}
					time.Sleep(time.Second * time.Duration(retry+1))
					continue
				}
				fmt.Printf("Metrics sent successfully. Message: %s\n", resp.Message)
				break
			}

			time.Sleep(1 * time.Second)
		}
	}
}

func (m *Monitor) RegisterWithServer() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &c4d.RegisterRequest{
		NodeId:     os.Getenv("TASK_ID"),
		NodeUrl:    m.nodeURL,
		NodeStatus: m.nodeStatus,
	}

	// Implement retry logic for registration
	maxRetries := 3
	for retry := 0; retry < maxRetries; retry++ {
		resp, err := m.grpcClient.Register(ctx, req)
		if err != nil {
			if retry == maxRetries-1 {
				return fmt.Errorf("failed to register after %d retries: %v", maxRetries, err)
			}
			time.Sleep(time.Second * time.Duration(retry+1))
			continue
		}
		fmt.Printf("Registration successful: %s\n", resp.Message)
		return nil
	}
	return nil
}

func (m *Monitor) handleRestart(c *gin.Context) {
	var config RestartConfig
	if err := c.ShouldBindJSON(&config); err != nil {
		fmt.Printf("Error binding restart config: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid config: %v", err)})
		return
	}
	fmt.Printf("Received restart config: %+v\n", config)

	m.rankMutex.Lock()
	defer m.rankMutex.Unlock()

	// Store old configuration for logging
	oldRank := m.rank
	oldWorldSize := m.worldSize

	// Update configuration
	m.rank = config.NewRank
	m.worldSize = config.WorldSize

	// Update active nodes
	m.activeNodes = make(map[string]NodeInfo)
	for _, nodeData := range config.ActiveNodes {
		nodeInfo := NodeInfo{
			Rank:   nodeData["rank"].(int),
			URL:    nodeData["url"].(string),
			NodeID: nodeData["node_id"].(string),
		}
		m.activeNodes[nodeInfo.NodeID] = nodeInfo
	}

	// Log configuration change
	fmt.Printf("Configuration updated: rank %d -> %d, world size %d -> %d\n",
		oldRank, m.rank, oldWorldSize, m.worldSize)

	// Stop current agent
	if err := m.stopAgent(); err != nil {
		fmt.Printf("Error stopping agent: %v\n", err)
	}

	// Update environment variables for the new configuration
	m.distEnvVars["RANK"] = fmt.Sprintf("%d", m.rank)
	m.distEnvVars["WORLD_SIZE"] = fmt.Sprintf("%d", m.worldSize)

	// Restart agent with new configuration
	if err := m.restartAgent(m.distEnvVars, "launch.sh"); err != nil {
		fmt.Printf("Error restarting agent: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to restart agent"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Restart completed successfully"})
}

// --- Lifecycle Management ---

func (m *Monitor) Start() {
	// Start the agent with initial settings
	initialEnvVars := map[string]string{}
	if err := m.startAgent(initialEnvVars, "launch.sh"); err != nil {
		fmt.Printf("Failed to start agent: %v\n", err)
	}

	go m.UpdateMetrics()
	go m.SendMetricsAndCCLLogsToServer()
	fmt.Println("Monitor started.")
}

func (m *Monitor) Stop() {
	// Stop the agent
	if err := m.stopAgent(); err != nil {
		fmt.Printf("Failed to stop agent: %v\n", err)
	}

	close(m.stopEvent)
	fmt.Println("Monitor stopped.")
}

// --- Gin Handlers ---

func (m *Monitor) updateLatencyWindow(remoteRank int, latency float64) float64 {
	m.latencyMutex.Lock()
	defer m.latencyMutex.Unlock()

	if _, exists := m.latencyWindow[remoteRank]; !exists {
		m.latencyWindow[remoteRank] = make([]float64, 0, m.windowSize)
	}

	// Add new latency
	m.latencyWindow[remoteRank] = append(m.latencyWindow[remoteRank], latency)

	// Keep window size fixed
	if len(m.latencyWindow[remoteRank]) > m.windowSize {
		m.latencyWindow[remoteRank] = m.latencyWindow[remoteRank][1:]
	}

	// Calculate average
	var sum float64
	for _, l := range m.latencyWindow[remoteRank] {
		sum += l
	}
	return sum / float64(len(m.latencyWindow[remoteRank]))
}

func (m *Monitor) handleLogCCL(c *gin.Context) {
	var incomingLog CCLLog
	if err := c.ShouldBindJSON(&incomingLog); err != nil {
		fmt.Printf("Error binding JSON: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid data: %v", err)})
		return
	}

	// Only process completed P2P operations
	if incomingLog.Finished && (incomingLog.OpType == "send" || incomingLog.OpType == "recv") {
		latency := float64(incomingLog.EndTime-incomingLog.StartTime) / 1000000 // Convert to ms
		m.updateLatencyWindow(int(incomingLog.RemoteRank), latency)
	}

	c.JSON(http.StatusOK, gin.H{"message": "Log processed"})
}

func (m *Monitor) handleActivate(c *gin.Context) {
	m.isStandby = false
	m.nodeStatus = "active"
	m.Start()
	c.JSON(http.StatusOK, gin.H{"message": "Node activated"})
}

func (m *Monitor) handleRemove(c *gin.Context) {
	m.Stop()
	m.isStandby = true
	m.nodeStatus = "standby"
	c.JSON(http.StatusOK, gin.H{"message": "Node removed"})
}

func (m *Monitor) handleMetrics(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"ram_usage":             m.ramUsage,
		"cpu_usage":             m.cpuUsage,
		"compute_engine_status": m.computeEngineStatus,
		"agent_metrics":         m.agentMetrics,
		"agent_status":          m.agentStatus,
	})
}

func (m *Monitor) GetCurrentRank() int {
	m.rankMutex.RLock()
	defer m.rankMutex.RUnlock()
	return m.rank
}

func (m *Monitor) GetWorldSize() int {
	m.rankMutex.RLock()
	defer m.rankMutex.RUnlock()
	return m.worldSize
}

func (m *Monitor) GetNodeInfo(nodeID string) (NodeInfo, bool) {
	m.rankMutex.RLock()
	defer m.rankMutex.RUnlock()
	info, exists := m.activeNodes[nodeID]
	return info, exists
}

// --- Main Function ---

func main() {
	monitor, err := NewMonitor()
	if err != nil {
		fmt.Printf("Failed to create monitor: %v\n", err)
		os.Exit(1)
	}

	// Register with server
	if err := monitor.RegisterWithServer(); err != nil {
		fmt.Printf("Failed to register with server: %v\n", err)
		os.Exit(1)
	}

	if !monitor.isStandby {
		monitor.Start()
	}

	r := gin.Default()
	r.POST("/log_ccl", monitor.handleLogCCL)
	r.POST("/activate", monitor.handleActivate)
	r.POST("/remove", monitor.handleRemove)
	r.GET("/metrics", monitor.handleMetrics)
	r.POST("/restart", monitor.handleRestart)

	// Shutdown handler
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
		<-sig
		fmt.Println("Shutting down monitor...")
		monitor.Stop()
		os.Exit(0)
	}()

	r.Run(":8081")
}
