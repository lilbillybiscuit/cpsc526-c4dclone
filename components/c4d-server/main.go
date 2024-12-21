package main

import (
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

func (c *C4DServer) CollectMetrics() {
	for {
		c.Mutex.Lock()
		for nodeID, metrics := range c.NodeMetrics {
			resp, err := http.Get(fmt.Sprintf("%s/metrics", metrics.NodeURL))
			if err != nil {
				c.NodeStatuses[nodeID] = "unresponsive"
				delete(c.NodeMetrics, nodeID)
				delete(c.FailureProbabilities, nodeID)
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
			c.NodeStatuses[nodeID] = data["compute_engine_status"].(string)
		}
		c.Mutex.Unlock()
		c.CalculateFailureProbabilities()
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
	// go c4dServer.CollectMetrics()

	r := gin.Default()

	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "C4D Server is running"})
	})

	r.POST("/register", func(c *gin.Context) {
		var data struct {
			NodeID  string `json:"node_id"`
			NodeURL string `json:"node_url"`
		}
		if err := c.ShouldBindJSON(&data); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid data"})
			return
		}
		c4dServer.RegisterNode(data.NodeID, data.NodeURL)
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
