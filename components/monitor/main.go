package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

type Monitor struct {
	StopEvent           chan bool
	RamUsage            float64
	CpuUsage            float64
	ComputeEngineStatus string
	Metrics             map[string][]interface{}
	C4DServerURL        string
	IsStandby           bool
	NodeStatus          string
	DistEnvVars         map[string]string
	CCLLogs             []map[string]interface{}
	CCLLock             sync.Mutex
}

func NewMonitor() *Monitor {
	isStandby := rand.Float64() < 0.25
	nodeStatus := "active"
	if isStandby {
		nodeStatus = "standby"
	}

	return &Monitor{
		StopEvent:           make(chan bool),
		RamUsage:            0,
		CpuUsage:            0,
		ComputeEngineStatus: "unknown",
		Metrics:             make(map[string][]interface{}),
		C4DServerURL:        "http://c4d-server.central-services:8091",
		IsStandby:           isStandby,
		NodeStatus:          nodeStatus,
		DistEnvVars: map[string]string{
			"MASTER_ADDR": os.Getenv("MASTER_ADDR"),
			"MASTER_PORT": os.Getenv("MASTER_PORT"),
			"WORLD_SIZE":  os.Getenv("WORLD_SIZE"),
			"RANK":        os.Getenv("RANK"),
			"TASK_ID":     os.Getenv("TASK_ID"),
		},
		CCLLogs: []map[string]interface{}{},
	}
}

// Append a metric
func (m *Monitor) AppendMetric(key string, value interface{}) {
	m.Metrics[key] = append(m.Metrics[key], value)
}

// Append a CCL log
func (m *Monitor) AppendCCLLog(log map[string]interface{}) {
	m.CCLLock.Lock()
	defer m.CCLLock.Unlock()
	m.CCLLogs = append(m.CCLLogs, log)
}

// Get all CCL logs
func (m *Monitor) GetCCLLogs() []map[string]interface{} {
	m.CCLLock.Lock()
	defer m.CCLLock.Unlock()
	logsCopy := make([]map[string]interface{}, len(m.CCLLogs))
	copy(logsCopy, m.CCLLogs)
	return logsCopy
}

// Clear CCL logs
func (m *Monitor) ClearCCLLogs() {
	m.CCLLock.Lock()
	defer m.CCLLock.Unlock()
	m.CCLLogs = []map[string]interface{}{}
}

// Fetch a node's PID
func GetNodePID(nodeID string) (int, error) {
	url := fmt.Sprintf("http://%s:8081/pid", nodeID)
	resp, err := http.Get(url)
	if err != nil {
		return 0, fmt.Errorf("error fetching PID: %w", err)
	}
	defer resp.Body.Close()

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0, fmt.Errorf("error decoding PID response: %w", err)
	}

	pid, ok := data["pid"].(float64)
	if !ok {
		return 0, fmt.Errorf("invalid PID format")
	}

	return int(pid), nil
}

// Send a POST request
func sendPostRequest(url string, payload map[string]interface{}) (*http.Response, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON: %w", err)
	}
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	return client.Do(req)
}

// Start monitoring
func (m *Monitor) Start() {
	go m.UpdateMetrics()
	go m.SendMetricsToServer()
	fmt.Println("Monitor started.")
}

// Stop monitoring
func (m *Monitor) Stop() {
	close(m.StopEvent)
	fmt.Println("Monitor stopped.")
}

// Update metrics
func (m *Monitor) UpdateMetrics() {
	for {
		select {
		case <-m.StopEvent:
			return
		default:
			m.RamUsage = float64(rand.Intn(100))
			m.CpuUsage = float64(rand.Intn(100))
			m.ComputeEngineStatus = "healthy"
			time.Sleep(1 * time.Second)
		}
	}
}

// Send metrics to server
func (m *Monitor) SendMetricsToServer() {
	for {
		select {
		case <-m.StopEvent:
			return
		default:
			payload := map[string]interface{}{
				"node_id": os.Getenv("TASK_ID"),
				"metrics": map[string]interface{}{
					"cpu_usage": []float64{m.CpuUsage},
					"ram_usage": []float64{m.RamUsage},
				},
			}

			resp, err := sendPostRequest(fmt.Sprintf("%s/metrics", m.C4DServerURL), payload)
			if err != nil {
				fmt.Printf("Error sending metrics: %v\n", err)
			} else {
				fmt.Printf("Metrics sent successfully: %d\n", resp.StatusCode)
			}

			time.Sleep(2 * time.Second)
		}
	}
}

func (m *Monitor) RegisterWithServer() {
	payload := map[string]interface{}{
		"node_id":     os.Getenv("TASK_ID"),
		"node_url":    fmt.Sprintf("http://%s.%s:8081", os.Getenv("TASK_ID"), os.Getenv("NAMESPACE")),
		"node_status": m.NodeStatus,
	}

	resp, err := sendPostRequest(fmt.Sprintf("%s/register", m.C4DServerURL), payload)
	if err != nil {
		fmt.Printf("Error registering node: %v\n", err)
		return
	}

	if resp.StatusCode == http.StatusOK {
		fmt.Printf("Node registered successfully: %d\n", resp.StatusCode)
	} else {
		fmt.Printf("Failed to register node. Status code: %d\n", resp.StatusCode)
	}
}

// Main function
func main() {
	monitor := NewMonitor()
	monitor.RegisterWithServer()

	if !monitor.IsStandby {
		monitor.Start()
	}

	r := gin.Default()

	// Get metrics
	r.GET("/metrics", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"ram_usage":             monitor.RamUsage,
			"cpu_usage":             monitor.CpuUsage,
			"compute_engine_status": monitor.ComputeEngineStatus,
		})
	})

	// Get environment variables
	r.GET("/env", func(c *gin.Context) {
		c.JSON(http.StatusOK, monitor.DistEnvVars)
	})

	// Log CCL
	r.POST("/log_ccl", func(c *gin.Context) {
		var data map[string]interface{}
		if err := c.ShouldBindJSON(&data); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid data"})
			return
		}
		monitor.AppendCCLLog(data)
		c.JSON(http.StatusOK, gin.H{"message": "CCL log appended"})
	})

	// Log training time
	r.POST("/log_training_time", func(c *gin.Context) {
		var data struct {
			IterationTime float64 `json:"iteration_time"`
			Iteration     int     `json:"iteration"`
		}
		if err := c.ShouldBindJSON(&data); err != nil || data.IterationTime == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid data"})
			return
		}
		monitor.AppendMetric("training_time", data.IterationTime)
		c.JSON(http.StatusOK, gin.H{"message": "Training time logged", "iteration": data.Iteration})
	})

	// Activate node
	r.POST("/activate", func(c *gin.Context) {
		monitor.IsStandby = false
		monitor.NodeStatus = "active"
		monitor.Start()
		c.JSON(http.StatusOK, gin.H{"message": "Node activated and ready to participate."})
	})

	// Remove node
	r.POST("/remove", func(c *gin.Context) {
		monitor.Stop()
		monitor.IsStandby = true
		monitor.NodeStatus = "standby"
		c.JSON(http.StatusOK, gin.H{"message": "Node removed from training."})
	})

	// Offload task
	r.POST("/offload", func(c *gin.Context) {
		var data struct {
			TargetNodeID   string `json:"target_node_id"`
			CheckpointPath string `json:"checkpoint_path"`
		}
		if err := c.ShouldBindJSON(&data); err != nil || data.TargetNodeID == "" || data.CheckpointPath == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid offload request"})
			return
		}
		nodePID, err := GetNodePID(data.TargetNodeID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch node PID"})
			return
		}
		if err := syscall.Kill(nodePID, syscall.SIGUSR2); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to send signal: %v", err)})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Offload signal sent"})
	})

	// Get PID
	r.GET("/pid", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"pid": os.Getpid()})
	})

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
