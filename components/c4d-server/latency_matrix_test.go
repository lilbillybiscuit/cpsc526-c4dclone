package main

import (
	"c4d-server/proto"
	"context"
	"fmt"
	"testing"
)

// Helper function to simulate monitorNodes logic
func runMonitorLogic(server *C4DServer) {
	server.checkNodeHealth()
	server.analyzeLatencies()
}

func TestLatencyDetection(t *testing.T) {
	expectedNodes := 5
	standbyNodes := 2

	server := NewC4DServer(expectedNodes, standbyNodes)

	// Simulate node registration
	for i := 0; i < expectedNodes; i++ {
		nodeID := fmt.Sprintf("node%d", i)
		nodeURL := fmt.Sprintf("http://localhost:%d", 8091+i)

		_, err := server.Register(context.Background(), &c4d.RegisterRequest{
			NodeId:     nodeID,
			NodeUrl:    nodeURL,
			NodeStatus: "active",
		})
		if err != nil {
			t.Fatalf("Failed to register node %s: %v", nodeID, err)
		}
	}

	// Helper function to update latency matrix
	updateLatencyMatrix := func(matrix [][]float64) {
		for senderRank, row := range matrix {
			for receiverRank, latency := range row {
				server.latencyMatrix.UpdateLatency(senderRank, receiverRank, latency)
			}
		}
	}

	// Test case 1: Normal latencies, no high latency detected
	normalLatencyMatrix := [][]float64{
		{0, 10, 20, 30, 40},
		{15, 0, 25, 35, 45},
		{20, 25, 0, 30, 40},
		{30, 35, 30, 0, 10},
		{40, 45, 40, 15, 0},
	}
	updateLatencyMatrix(normalLatencyMatrix)

	// Run monitor logic
	runMonitorLogic(server)

	// Check if any nodes are marked as failed
	for _, node := range server.nodes {
		if node.Status == "failed" {
			t.Errorf("Node %s incorrectly marked as failed", node.NodeURL)
		}
	}

	// Test case 2: High latency detected
	highLatencyMatrix := [][]float64{
		{0, 150, 20, 30, 40},
		{15, 0, 25, 35, 45},
		{20, 25, 0, 30, 40},
		{30, 35, 30, 0, 10},
		{40, 45, 40, 15, 0},
	}
	updateLatencyMatrix(highLatencyMatrix)

	// Run monitor logic
	runMonitorLogic(server)

	// Check if node with high latency is marked as failed
	highLatencyNode := "node0"
	expectedStatus := "failed"
	actualStatus := server.nodes[highLatencyNode].Status
	if actualStatus != expectedStatus {
		t.Errorf("Node %s should be marked as %s, but is marked as %s", highLatencyNode, expectedStatus, actualStatus)
	}
}

func TestReassignRanks(t *testing.T) {
	expectedNodes := 5
	standbyNodes := 2

	server := NewC4DServer(expectedNodes, standbyNodes)

	// Simulate node registration
	for i := 0; i < expectedNodes; i++ {
		nodeID := fmt.Sprintf("node%d", i)
		nodeURL := fmt.Sprintf("http://localhost:%d", 8091+i)

		_, err := server.Register(context.Background(), &c4d.RegisterRequest{
			NodeId:     nodeID,
			NodeUrl:    nodeURL,
			NodeStatus: "active",
		})
		if err != nil {
			t.Fatalf("Failed to register node %s: %v", nodeID, err)
		}
	}

	// Simulate a node failure
	failedNode := "node0"
	server.handleNodeFailure(failedNode)

	// Check if ranks are reassigned correctly
	expectedRanks := map[string]int{
		"node1": 0,
		"node2": 1,
		"node3": 2,
		"node4": 3,
	}

	for nodeID, expectedRank := range expectedRanks {
		actualRank := server.nodes[nodeID].Rank
		if actualRank != expectedRank {
			t.Errorf("Node %s should have rank %d, but has rank %d", nodeID, expectedRank, actualRank)
		}
	}

	// Ensure the failed node has been re-assigned a rank of -1
	if server.nodes[failedNode].Rank != -1 {
		t.Errorf("Failed node %s should have rank -1, but has rank %d", failedNode, server.nodes[failedNode].Rank)
	}
}
