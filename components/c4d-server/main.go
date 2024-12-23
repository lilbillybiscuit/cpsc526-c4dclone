package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type NodeMetrics struct {
	Latency     []float64
	MemoryUsage []float64
	NodeURL     string
}

type C4DServer struct {
	NodeMetrics          map[string]*NodeMetrics
	NodeStatuses         map[string]string
	FailureProbabilities map[string]float64
	Mutex                sync.Mutex
}

func NewC4DServer() *C4DServer {
	return &C4DServer{
		NodeMetrics:          make(map[string]*NodeMetrics),
		NodeStatuses:         make(map[string]string),
		FailureProbabilities: make(map[string]float64),
	}
}

func (c *C4DServer) UpdateNodeStatus(nodeID, status string) {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()
	c.NodeStatuses[nodeID] = status
}

func (c *C4DServer) GetNodeStatus(nodeID string) string {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()
	return c.NodeStatuses[nodeID]
}

func (c *C4DServer) CalculateFailureProbabilities() {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()
	for nodeID, metrics := range c.NodeMetrics {
		latencyScore := 0.0
		if len(metrics.Latency) > 0 {
			for _, l := range metrics.Latency {
				latencyScore += l
			}
			latencyScore /= float64(len(metrics.Latency))
		}

		memoryScore := 0.0
		if len(metrics.MemoryUsage) > 0 {
			for _, m := range metrics.MemoryUsage {
				memoryScore += m
			}
			memoryScore /= float64(len(metrics.MemoryUsage))
		}

		failureProbability := 0.1*latencyScore + 0.005*memoryScore + rand.Float64()*0.05
		if failureProbability > 1.0 {
			failureProbability = 1.0
		}
		c.FailureProbabilities[nodeID] = failureProbability
	}
}

func sendPostRequest(url string, payload map[string]string) error {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send POST request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("received non-OK response: %s", resp.Status)
	}
	fmt.Println("POST request successful:", url)
	return nil
}

func (c *C4DServer) HandleNodeFailure() {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	originalWorldSize := len(c.NodeMetrics)

	failedNodes := []string{}

	// Detect nodes with high failure probabilities or unresponsive nodes
	for nodeID, probability := range c.FailureProbabilities {
		if probability > 0.8 || c.NodeStatuses[nodeID] == "unresponsive" {
			failedNodes = append(failedNodes, nodeID)
		}
	}

	for _, nodeID := range failedNodes {
		// Notify the node that it's being removed
		go c.NotifyNodeRemoval(nodeID)

		// Remove the node from the system
		delete(c.NodeMetrics, nodeID)
		delete(c.NodeStatuses, nodeID)
		delete(c.FailureProbabilities, nodeID)
		fmt.Printf("Node %s removed from the system.\n", nodeID)
	}

	// Update world size and leader
	activeNodes := []string{}
	for nodeID, status := range c.NodeStatuses {
		if status == "active" {
			activeNodes = append(activeNodes, nodeID)
		}
	}
	worldSize := len(activeNodes)
	fmt.Printf("Updated world size: %d\n", worldSize)

	if worldSize > 0 {
		// Assign new leader if needed
		leaderID := activeNodes[0] // Choose the node with the lowest rank (first in list)
		c.AssignLeader(leaderID)
	}

	// Activate standby nodes to replace removed nodes
	for _, nodeID := range c.GetStandbyNodes() {
		if worldSize < originalWorldSize {
			go c.ActivateNode(nodeID)
			worldSize++
		}
	}
}

// Notify the node that it's being removed
func (c *C4DServer) NotifyNodeRemoval(nodeID string) {
	// TODO retrieve actual node url from c.NodeMetrics (this is incorrect and will error)
	url := fmt.Sprintf("http://%s:8081/remove", nodeID)
	payload := map[string]string{"reason": "High failure probability or unresponsive"}
	sendPostRequest(url, payload)
}

// Assign the new leader
func (c *C4DServer) AssignLeader(leaderID string) {
	c.NodeStatuses[leaderID] = "leader"
	fmt.Printf("Node %s assigned as the new leader.\n", leaderID)
}

// Activate a standby node
func (c *C4DServer) ActivateNode(nodeID string) {
	url := fmt.Sprintf("http://%s:8081/activate", nodeID)
	payload := map[string]string{"action": "activate"}
	sendPostRequest(url, payload)
	c.NodeStatuses[nodeID] = "active"
	fmt.Printf("Node %s activated from standby.\n", nodeID)
}

// Get list of standby nodes
func (c *C4DServer) GetStandbyNodes() []string {
	standbyNodes := []string{}
	for nodeID, status := range c.NodeStatuses {
		if status == "standby" {
			standbyNodes = append(standbyNodes, nodeID)
		}
	}
	return standbyNodes
}

func (c *C4DServer) CollectMetrics() {
	// wait 30 seconds to allow all nodes to register
	time.Sleep(30 * time.Second)
	for {
		c.Mutex.Lock()
		for nodeID, metrics := range c.NodeMetrics {
			client := &http.Client{
				Timeout: 10 * time.Second,
			}
			resp, err := client.Get(fmt.Sprintf("%s/metrics", metrics.NodeURL))
			if err != nil {
				fmt.Printf("Node %s is unresponsive at: %s/metrics\n", nodeID, metrics.NodeURL)
				// c.NodeStatuses[nodeID] = "unresponsive"
				// delete(c.NodeMetrics, nodeID)
				// delete(c.FailureProbabilities, nodeID)
				continue
			}
			defer resp.Body.Close()

			var data map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
				continue
			}

			latency := time.Since(time.Now()).Seconds()
			metrics.Latency = append(metrics.Latency, latency)
			metrics.MemoryUsage = append(metrics.MemoryUsage, data["ram_usage"].(float64))

			if len(metrics.Latency) > 10 {
				metrics.Latency = metrics.Latency[len(metrics.Latency)-10:]
			}
			if len(metrics.MemoryUsage) > 10 {
				metrics.MemoryUsage = metrics.MemoryUsage[len(metrics.MemoryUsage)-10:]
			}
			// c.NodeStatuses[nodeID] = data["compute_engine_status"].(string)
		}
		c.Mutex.Unlock()
		c.CalculateFailureProbabilities()
		c.HandleNodeFailure()
		time.Sleep(10 * time.Second)
	}
}

func (c *C4DServer) RegisterNode(nodeID, nodeURL string) {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()
	c.NodeMetrics[nodeID] = &NodeMetrics{
		Latency:     []float64{},
		MemoryUsage: []float64{},
		NodeURL:     nodeURL,
	}
}

func (c *C4DServer) GetAggregatedMetrics() map[string]interface{} {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	totalNodes := len(c.NodeMetrics)
	totalLatency := 0.0
	totalMemoryUsage := 0.0

	for _, metrics := range c.NodeMetrics {
		if len(metrics.Latency) > 0 {
			totalLatency += metrics.Latency[len(metrics.Latency)-1]
		}
		if len(metrics.MemoryUsage) > 0 {
			totalMemoryUsage += metrics.MemoryUsage[len(metrics.MemoryUsage)-1]
		}
	}

	return map[string]interface{}{
		"total_nodes":           totalNodes,
		"average_latency":       totalLatency / float64(totalNodes),
		"average_memory_usage":  totalMemoryUsage / float64(totalNodes),
		"failure_probabilities": c.FailureProbabilities,
	}
}

func main() {
	c4dServer := NewC4DServer()
	go c4dServer.CollectMetrics()

	r := gin.Default()

	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "C4D Server is running"})
	})

	r.POST("/register", func(c *gin.Context) {
		var data struct {
			NodeID     string `json:"node_id"`
			NodeURL    string `json:"node_url"`
			NodeStatus string `json:"node_status"`
		}
		if err := c.ShouldBindJSON(&data); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid data"})
			return
		}
		c4dServer.RegisterNode(data.NodeID, data.NodeURL)
		c4dServer.UpdateNodeStatus(data.NodeID, data.NodeStatus)
		c.JSON(http.StatusOK, gin.H{"message": "Node registered", "node_id": data.NodeID})
	})

	r.GET("/failure_probabilities", func(c *gin.Context) {
		c.JSON(http.StatusOK, c4dServer.FailureProbabilities)
	})

	r.GET("/nodes/:node_id/status", func(c *gin.Context) {
		nodeID := c.Param("node_id")
		status := c4dServer.GetNodeStatus(nodeID)
		if status == "" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Node not found"})
		} else {
			c.JSON(http.StatusOK, gin.H{"node_id": nodeID, "status": status})
		}
	})

	r.POST("/metrics", func(c *gin.Context) {
		var data struct {
			NodeID  string               `json:"node_id"`
			Metrics map[string][]float64 `json:"metrics"`
		}
		if err := c.ShouldBindJSON(&data); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		c4dServer.Mutex.Lock()
		defer c4dServer.Mutex.Unlock()

		nodeMetrics, exists := c4dServer.NodeMetrics[data.NodeID]
		if !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": "Node not registered"})
			return
		}

		for key, values := range data.Metrics {
			if key == "latency" {
				nodeMetrics.Latency = append(nodeMetrics.Latency, values...)
				if len(nodeMetrics.Latency) > 10 {
					nodeMetrics.Latency = nodeMetrics.Latency[len(nodeMetrics.Latency)-10:]
				}
			} else if key == "memory_usage" {
				nodeMetrics.MemoryUsage = append(nodeMetrics.MemoryUsage, values...)
				if len(nodeMetrics.MemoryUsage) > 10 {
					nodeMetrics.MemoryUsage = nodeMetrics.MemoryUsage[len(nodeMetrics.MemoryUsage)-10:]
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{"message": "Metrics received"})
	})

	r.GET("/aggregated_metrics", func(c *gin.Context) {
		aggregatedMetrics := c4dServer.GetAggregatedMetrics()
		c.JSON(http.StatusOK, aggregatedMetrics)
	})

	r.Run(":8091")
}
