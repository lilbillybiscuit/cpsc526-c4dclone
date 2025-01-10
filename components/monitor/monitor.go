package main

import (
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

	"monitor/proto"
)

func NewMonitor() (*Monitor, error) {
	taskID := os.Getenv("TASK_ID")
	namespace := os.Getenv("NAMESPACE")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	var conn *grpc.ClientConn
	var err error
	for retries := 0; retries < MaxRetries; retries++ {
		conn, err = grpc.NewClient(ServerBaseURL,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithDefaultServiceConfig(GRPCServiceConfig),
			grpc.WithKeepaliveParams(keepalive.ClientParameters{
				Time:                GRPCKeepAliveTime,
				Timeout:             GRPCKeepAliveTimeout,
				PermitWithoutStream: true,
			}))
		if err == nil {
			break
		}
		time.Sleep(InitialBackoff)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %v", err)
	}

	rank, _ := strconv.Atoi(os.Getenv("RANK"))
	worldSize, _ := strconv.Atoi(os.Getenv("WORLD_SIZE"))

	monitor := &Monitor{
		nodeID:        taskID + "." + namespace + "." + port,
		nodeURL:       fmt.Sprintf("http://%s.%s:%s", taskID, namespace, port),
		rank:          rank,
		worldSize:     worldSize,
		state:         Initializing,
		latencyWindow: make(map[int][]float64),
		stopChan:      make(chan struct{}),
		grpcClient:    c4d.NewC4DServiceClient(conn),
	}

	return monitor, nil
}

func (m *Monitor) setState(newState State) {
	m.stateMux.Lock()
	defer m.stateMux.Unlock()

	oldState := m.state
	m.state = newState

	fmt.Printf("State transition: %v -> %v (at %s)\n",
		oldState, newState, time.Now().Format(time.RFC3339))

	if newState == Failed {
		debug.PrintStack()
	}

	switch newState {
	case Running:
		m.startHeartbeat()
		go m.sendMetrics()
	case Standby:
		if oldState == Running {
			close(m.stopChan)
			m.stopChan = make(chan struct{})
		}
		m.startHeartbeat()
	case Restarting:
		close(m.stopChan)
		m.stopChan = make(chan struct{})
		m.latencyWindow = make(map[int][]float64)
		m.startHeartbeat()
	case Failed:
		m.stopHeartbeat()
		m.notifyFailure()
	case Initializing:
		m.stopHeartbeat() // TODO: may be incorrect
	}
}

func (m *Monitor) getState() State {
	m.stateMux.RLock()
	defer m.stateMux.RUnlock()
	return m.state
}

func (m *Monitor) getEnvVars() map[string]string {
	return map[string]string{
		"RANK":        fmt.Sprintf("%d", m.rank),
		"WORLD_SIZE":  fmt.Sprintf("%d", m.worldSize),
		"MASTER_ADDR": os.Getenv("MASTER_ADDR"),
		"MASTER_PORT": os.Getenv("MASTER_PORT"),
		"TASK_ID":     m.nodeID,
	}
}

func (m *Monitor) Start() error {
	if m.getState() != Initializing {
		return fmt.Errorf("invalid state for start")
	}

	// Register with server
	var lastErr error
	for retries := 0; retries < MaxRetries; retries++ {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		resp, err := m.grpcClient.Register(ctx, &c4d.RegisterRequest{
			NodeId:     m.nodeID,
			NodeUrl:    m.nodeURL,
			NodeStatus: "standby",
			MasterAddr: os.Getenv("MASTER_ADDR"),
			MasterPort: os.Getenv("MASTER_PORT"),
		})
		cancel()

		if err != nil {
			lastErr = err
			if strings.Contains(err.Error(), "context deadline exceeded") ||
				strings.Contains(err.Error(), "Reconfiguring") {
				fmt.Printf("Registration attempt %d/%d failed: %v\n", retries+1, MaxRetries, err)
				time.Sleep(InitialBackoff * time.Duration(retries+1))
				continue
			}
			return err
		}

		fmt.Printf("Registration successful: %s\n", resp.Message)
		break
	}

	if lastErr != nil {
		m.setState(Failed)
		return fmt.Errorf("failed to register after %d attempts: %v", MaxRetries, lastErr)
	}

	m.setState(Standby)
	return nil
}

func (m *Monitor) Stop() {
	m.setState(Failed) // This will stop the heartbeat

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &c4d.MetricsRequest{
		NodeId: m.nodeID,
		Status: &c4d.NodeStatus{
			IsAlive: false,
			Error:   "Node shutting down gracefully",
		},
	}

	if _, err := m.grpcClient.SendMetrics(ctx, req); err != nil {
		fmt.Printf("Failed to send shutdown notification: %v\n", err)
	}
}

func (m *Monitor) notifyFailure() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &c4d.FailureNotificationRequest{
		NodeId:       m.nodeID,
		ErrorMessage: "Node entered failed state",
		Timestamp:    time.Now().UnixNano(),
		ProcessId:    0,
		FailureType:  "state_transition",
	}

	if _, err := m.grpcClient.NotifyFailure(ctx, req); err != nil {
		fmt.Printf("Failed to notify server of failure: %v\n", err)
	}
}

func (m *Monitor) handleCommand(c *gin.Context) {
	var cmd struct {
		Command   string            `json:"command"`
		EnvVars   map[string]string `json:"env_vars"`
		Arguments map[string]string `json:"arguments,omitempty"`
	}

	if err := c.ShouldBindJSON(&cmd); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid command format"})
		return
	}

	switch cmd.Command {
	case "start":
		currentState := m.getState()
		if currentState != Standby {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": fmt.Sprintf("Cannot start: node in %s state", currentState),
			})
			return
		}

		if err := m.startAgent(cmd.EnvVars); err != nil {
			m.setState(Failed)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		m.setState(Running)
		c.JSON(http.StatusOK, gin.H{"message": "Agent started successfully"})

	case "stop":
		if err := m.stopAgent(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		m.setState(Standby)
		c.JSON(http.StatusOK, gin.H{"message": "Agent stopped successfully"})

	case "restart":
		currentState := m.getState()
		// Allow restart from Running, Restarting, and Standby states
		if currentState != Running && currentState != Restarting && currentState != Standby {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": fmt.Sprintf("Cannot restart: node in %s state", currentState),
			})
			return
		}

		m.setState(Restarting)

		// If currently running, stop first
		if currentState == Running {
			if err := m.stopAgent(); err != nil {
				m.setState(Failed)
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to stop agent: %v", err)})
				return
			}
		}

		// Start with new environment variables
		if err := m.startAgent(cmd.EnvVars); err != nil {
			m.setState(Failed)
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to start agent: %v", err)})
			return
		}

		m.setState(Running)
		c.JSON(http.StatusOK, gin.H{"message": "Agent restarted successfully"})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unknown command"})
	}
}

// Heartbeats

func (m *Monitor) startHeartbeat() {
	if m.heartbeatTicker != nil {
		// Heartbeat already running
		return
	}

	m.heartbeatTicker = time.NewTicker(HeartbeatInterval)
	m.heartbeatDone = make(chan struct{})

	go func() {
		for {
			println("Heartbeat")
			select {
			case <-m.heartbeatDone:
				return
			case <-m.heartbeatTicker.C:
				currentState := m.getState()
				if currentState == Failed || currentState == Initializing {
					continue
				}

				ctx, cancel := context.WithTimeout(context.Background(), HeartbeatTimeout)
				req := &c4d.HeartbeatRequest{
					NodeId:    m.nodeID,
					Status:    currentState.String(),
					Timestamp: time.Now().UnixNano(),
				}

				_, err := m.grpcClient.SendHeartbeat(ctx, req)
				cancel()

				if err != nil {
					fmt.Printf("Heartbeat failed: %v\n", err)
					if currentState != Failed &&
						(strings.Contains(err.Error(), "connection refused") ||
							strings.Contains(err.Error(), "transport is closing")) {
						m.setState(Failed)
						return
					}
				}
			}
		}
	}()
}

func (m *Monitor) stopHeartbeat() {
	if m.heartbeatTicker != nil {
		m.heartbeatTicker.Stop()
		m.heartbeatTicker = nil
		close(m.heartbeatDone)
		m.heartbeatDone = nil
	}
}
