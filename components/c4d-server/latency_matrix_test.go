package main

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	c4d "c4d-server/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

type testNode struct {
	id       string
	url      string
	listener *bufconn.Listener
	server   *grpc.Server
	client   c4d.C4DServiceClient
	conn     *grpc.ClientConn
}

func setupTestServer(t *testing.T) (*C4DServer, *testNode, func()) {
	// Create server config
	config := &ServerConfig{
		ExpectedNodes:    3,
		StandbyNodes:     1,
		ActiveNodes:      2,
		HeartbeatTimeout: 2 * time.Second,
		LatencyThreshold: 100 * time.Millisecond,
		GRPCPort:         "0", // Let the system assign a port
		APIPort:          "0",
	}

	// Create and start server
	server := NewC4DServer(config)
	lis := bufconn.Listen(bufSize)

	// Start gRPC server
	go func() {
		if err := server.grpcServer.Serve(lis); err != nil {
			t.Logf("Server stopped: %v", err)
		}
	}()

	// Create test node
	node := &testNode{
		id:       "test-node-1",
		url:      "localhost:8081",
		listener: bufconn.Listen(bufSize),
	}

	// Setup client connection
	dialCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	conn, err := grpc.DialContext(dialCtx, "",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithInsecure(),
	)
	require.NoError(t, err)

	node.conn = conn
	node.client = c4d.NewC4DServiceClient(conn)

	cleanup := func() {
		conn.Close()
		lis.Close()
		server.Stop()
		if node.server != nil {
			node.server.Stop()
		}
		if node.listener != nil {
			node.listener.Close()
		}
	}

	return server, node, cleanup
}

func TestNodeFailureAndRestart(t *testing.T) {
	server, node, cleanup := setupTestServer(t)
	defer cleanup()

	// Test steps
	t.Run("1. Initial Registration", func(t *testing.T) {
		// Register node
		resp, err := node.client.Register(context.Background(), &c4d.RegisterRequest{
			NodeId:     node.id,
			NodeUrl:    node.url,
			NodeStatus: "active",
		})
		require.NoError(t, err)
		assert.Contains(t, resp.Message, "Node registered")
	})

	t.Run("2. Normal Operation", func(t *testing.T) {
		// Send healthy metrics
		_, err := node.client.SendMetrics(context.Background(), &c4d.MetricsRequest{
			NodeId: node.id,
			Status: &c4d.NodeStatus{
				IsAlive:   true,
				ProcessId: 1234,
			},
			Metrics: &c4d.Metrics{
				RamUsage: 50.0,
				CpuUsage: 30.0,
			},
		})
		require.NoError(t, err)

		// Verify node is registered and healthy
		nodeState, health, err := server.registry.GetNode(node.id)
		require.NoError(t, err)
		assert.Equal(t, "active", nodeState.Status)
		assert.True(t, time.Since(health.LastHeartbeat) < server.config.HeartbeatTimeout)
	})

	t.Run("3. Simulate Node Failure", func(t *testing.T) {
		// Send failure metrics
		_, err := node.client.SendMetrics(context.Background(), &c4d.MetricsRequest{
			NodeId: node.id,
			Status: &c4d.NodeStatus{
				IsAlive:   false,
				ProcessId: 1234,
			},
		})
		require.NoError(t, err)

		// Give server time to process failure
		time.Sleep(time.Second)

		// Verify node is marked as failed
		nodeState, health, err := server.registry.GetNode(node.id)
		require.NoError(t, err)
		assert.Equal(t, "failed", nodeState.Status)
		assert.Equal(t, "failed", health.Status)
	})

	t.Run("4. Node Restart and Recovery", func(t *testing.T) {
		// Simulate node restart by re-registering
		resp, err := node.client.Register(context.Background(), &c4d.RegisterRequest{
			NodeId:     node.id,
			NodeUrl:    node.url,
			NodeStatus: "active",
		})
		require.NoError(t, err)
		assert.Contains(t, resp.Message, "Node registered")

		// Send healthy metrics
		_, err = node.client.SendMetrics(context.Background(), &c4d.MetricsRequest{
			NodeId: node.id,
			Status: &c4d.NodeStatus{
				IsAlive:   true,
				ProcessId: 5678, // New process ID
			},
			Metrics: &c4d.Metrics{
				RamUsage: 40.0,
				CpuUsage: 20.0,
			},
		})
		require.NoError(t, err)

		// Verify node is healthy again
		nodeState, health, err := server.registry.GetNode(node.id)
		require.NoError(t, err)
		assert.Equal(t, "active", nodeState.Status)
		assert.Equal(t, int64(5678), health.ProcessID)
		assert.True(t, time.Since(health.LastHeartbeat) < server.config.HeartbeatTimeout)
	})

	t.Run("5. Verify Session Management", func(t *testing.T) {
		session, exists := server.sessionManager.GetSession(node.id)
		require.True(t, exists)
		assert.True(t, session.IsConnected)
		assert.Greater(t, session.ReconnectCount, 0)
	})

	t.Run("6. Verify Cluster Reconfiguration", func(t *testing.T) {
		// Get current cluster metrics
		metrics := server.metrics.metrics
		assert.Equal(t, int32(1), metrics.activeNodes.Load())
		assert.Equal(t, int32(0), metrics.failedNodes.Load())
	})
}

// Additional helper test to verify multiple node failures
func TestMultipleNodeFailuresAndRecovery(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Create multiple test nodes
	nodes := make([]*testNode, 3)
	for i := range nodes {
		node := &testNode{
			id:       fmt.Sprintf("test-node-%d", i+1),
			url:      fmt.Sprintf("localhost:808%d", i+1),
			listener: bufconn.Listen(bufSize),
		}

		// Setup connection for each node
		dialCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		conn, err := grpc.DialContext(dialCtx, "",
			grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
				return node.listener.Dial()
			}),
			grpc.WithInsecure(),
		)
		cancel()
		require.NoError(t, err)

		node.conn = conn
		node.client = c4d.NewC4DServiceClient(conn)
		nodes[i] = node

		defer func(n *testNode) {
			n.conn.Close()
			n.listener.Close()
		}(node)
	}

	t.Run("1. Register Multiple Nodes", func(t *testing.T) {
		for _, node := range nodes {
			resp, err := node.client.Register(context.Background(), &c4d.RegisterRequest{
				NodeId:     node.id,
				NodeUrl:    node.url,
				NodeStatus: "active",
			})
			require.NoError(t, err)
			assert.Contains(t, resp.Message, "Node registered")
		}

		assert.Equal(t, int32(3), server.metrics.metrics.activeNodes.Load())
	})

	t.Run("2. Simulate Multiple Node Failures", func(t *testing.T) {
		// Fail nodes 0 and 1
		for i := 0; i < 2; i++ {
			_, err := nodes[i].client.SendMetrics(context.Background(), &c4d.MetricsRequest{
				NodeId: nodes[i].id,
				Status: &c4d.NodeStatus{
					IsAlive:   false,
					ProcessId: 1234,
				},
			})
			require.NoError(t, err)
		}

		// Give server time to process failures
		time.Sleep(2 * time.Second)

		assert.Equal(t, int32(1), server.metrics.metrics.activeNodes.Load())
		assert.Equal(t, int32(2), server.metrics.metrics.failedNodes.Load())
	})

	t.Run("3. Recover Failed Nodes", func(t *testing.T) {
		// Recover failed nodes
		for i := 0; i < 2; i++ {
			resp, err := nodes[i].client.Register(context.Background(), &c4d.RegisterRequest{
				NodeId:     nodes[i].id,
				NodeUrl:    nodes[i].url,
				NodeStatus: "active",
			})
			require.NoError(t, err)
			assert.Contains(t, resp.Message, "Node registered")

			// Send healthy metrics
			_, err = nodes[i].client.SendMetrics(context.Background(), &c4d.MetricsRequest{
				NodeId: nodes[i].id,
				Status: &c4d.NodeStatus{
					IsAlive:   true,
					ProcessId: 5678,
				},
			})
			require.NoError(t, err)
		}

		// Give server time to process recovery
		time.Sleep(2 * time.Second)

		assert.Equal(t, int32(3), server.metrics.metrics.activeNodes.Load())
		assert.Equal(t, int32(0), server.metrics.metrics.failedNodes.Load())
	})

	t.Run("4. Verify Cluster Health After Recovery", func(t *testing.T) {
		for _, node := range nodes {
			nodeState, health, err := server.registry.GetNode(node.id)
			require.NoError(t, err)
			assert.Equal(t, "active", nodeState.Status)
			assert.True(t, time.Since(health.LastHeartbeat) < server.config.HeartbeatTimeout)
		}
	})
}
