package src

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"

	"c4d-server/proto"
)

type C4DServer struct {
	state          *ServerState
	registry       *NodeRegistry
	config         *ServerConfig
	metrics        *MetricsProcessor
	healthMonitor  *HealthMonitor
	clusterManager *ClusterManager // Add cluster manager
	logger         *log.Logger
	grpcServer     *grpc.Server
	stopChan       chan struct{}
	sessionManager *SessionManager

	c4d.UnimplementedC4DServiceServer
}

func NewC4DServer(config *ServerConfig) *C4DServer {
	registry := NewNodeRegistry()
	metrics := NewMetricsProcessor(config.ActiveNodes)

	server := &C4DServer{
		state:          NewServerState(),
		registry:       registry,
		config:         config,
		metrics:        metrics,
		sessionManager: NewSessionManager(),
		logger:         log.New(os.Stdout, "[C4D] ", log.LstdFlags),
		stopChan:       make(chan struct{}),
	}

	// Initialize health monitor with server reference
	server.healthMonitor = NewHealthMonitor(
		server,
		registry,
		config,
		metrics.metrics,
	)

	server.clusterManager = NewClusterManager(server)
	return server
}

func (s *C4DServer) Start() error {
	// Use the configured GRPC port
	lis, err := net.Listen("tcp", ":"+s.config.GRPCPort)
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}

	go s.cleanupDeadConnections()

	opts := []grpc.ServerOption{
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     15 * time.Minute,
			MaxConnectionAge:      30 * time.Minute,
			MaxConnectionAgeGrace: 5 * time.Second,
			Time:                  5 * time.Second,
			Timeout:               1 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             5 * time.Second,
			PermitWithoutStream: true,
		}),
	}

	s.grpcServer = grpc.NewServer(opts...)
	c4d.RegisterC4DServiceServer(s.grpcServer, s)

	go s.healthMonitor.Start()

	//if err := s.state.Transition(Running); err != nil {
	//	return fmt.Errorf("failed to transition to running state: %v", err)
	//}

	s.logger.Printf("Starting gRPC server on :%s", s.config.GRPCPort)
	return s.grpcServer.Serve(lis)
}

func (s *C4DServer) Stop() {
	s.registry.mu.RLock()
	for nodeID := range s.registry.nodes {
		s.sessionManager.DisconnectSession(nodeID)
	}
	s.registry.mu.RUnlock()

	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
	}
	close(s.stopChan)
	s.healthMonitor.Stop()
}

func (s *C4DServer) Register(ctx context.Context, req *c4d.RegisterRequest) (*c4d.RegisterResponse, error) {
	currentState := s.state.Current()
	if currentState == Failed {
		return nil, status.Error(codes.Unavailable, "server in failed state")
	}
	if currentState != Initializing && currentState != Running {
		return nil, status.Error(codes.Unavailable,
			fmt.Sprintf("server not accepting registrations in %s state", currentState))
	}

	if s.config.MasterAddr == "" {
		s.config.MasterAddr = req.MasterAddr
		s.config.MasterPort = req.MasterPort
	}

	s.logger.Printf("Received registration request from node: %s", req.NodeId)

	// Check for existing session first
	if existingSession, exists := s.sessionManager.GetSession(req.NodeId); exists {
		s.logger.Printf("Found existing session for node %s (reconnect count: %d)",
			req.NodeId, existingSession.ReconnectCount)

		s.registry.mu.Lock()
		// Update existing node state
		if node, exists := s.registry.nodes[req.NodeId]; exists {
			// Preserve existing node state
			s.sessionManager.UpdateSession(req.NodeId, func(session *SessionState) {
				session.NodeURL = req.NodeUrl
				session.LastSeen = time.Now()
				session.IsConnected = true
				session.ReconnectCount++
				session.Status = node.Status
				session.Rank = node.Rank
			})

			// Update health status
			s.registry.health[req.NodeId] = &NodeHealth{
				LastHeartbeat: time.Now(),
				Status:        node.Status,
				FailureCount:  0, // Reset failure count on successful reconnection
			}
			s.registry.mu.Unlock()
			println("Unlocking registry")

			return &c4d.RegisterResponse{
				Message: fmt.Sprintf("Node reconnected with existing rank %d (reconnect count: %d)",
					node.Rank, existingSession.ReconnectCount+1),
				NodeId: req.NodeId,
			}, nil
		}
		s.registry.mu.Unlock()
		println("Unlocking registry")
	}

	s.metrics.mu.Lock()

	activeCount := s.metrics.metrics.activeNodes.Load()
	standbyCount := s.metrics.metrics.standbyNodes.Load()
	//expectedActive := s.config.ExpectedNodes - s.config.StandbyNodes

	var nodeStatus string
	var nodeRank int

	if int(activeCount) < s.config.ActiveNodes {
		nodeStatus = "active"
		nodeRank = int(activeCount)
		s.metrics.metrics.activeNodes.Add(1)
	} else if int(standbyCount) < s.config.StandbyNodes {
		nodeStatus = "standby"
		nodeRank = -1
		s.metrics.metrics.standbyNodes.Add(1)
	} else {
		s.metrics.mu.Unlock()
		return nil, status.Error(codes.ResourceExhausted,
			fmt.Sprintf("maximum nodes reached (active: %d, standby: %d)",
				activeCount, standbyCount))
	}
	s.metrics.mu.Unlock()

	newSession := s.sessionManager.RegisterSession(req.NodeId, req.NodeUrl, nodeStatus, nodeRank)
	s.registry.RegisterNode(req.NodeId, req.NodeUrl, nodeStatus, nodeRank)

	fmt.Printf("Node %s registered as %s with rank %d (URL: %s) (session: %d)\n", req.NodeId, nodeStatus, nodeRank, req.NodeUrl, newSession.ReconnectCount)
	//
	//if err := s.clusterManager.checkAndStartProgram(); err != nil {
	//	s.logger.Printf("Failed to start program: %v", err)
	//	// Don't fail registration if program start fails
	//}

	//println("Currently registered nodes: ")
	//for k, v := range s.registry.nodes {
	//	println(k, v)
	//}

	totalNodes := s.metrics.metrics.activeNodes.Load() + s.metrics.metrics.standbyNodes.Load()
	if totalNodes == int32(s.config.ExpectedNodes) && s.state.Current() == Initializing {
		s.logger.Printf("All expected nodes registered (%d nodes), preparing to start programs", totalNodes)

		// Start programs through cluster manager
		if err := s.clusterManager.checkAndStartProgram(); err != nil {
			s.logger.Printf("Failed to start programs: %v", err)
			return nil, status.Error(codes.Internal, "failed to start programs")
		}

		// Only transition to Running after successful program start
		//if err := s.state.Transition(Running); err != nil {
		//	s.logger.Printf("Failed to transition to Running state: %v", err)
		//	return nil, status.Error(codes.Internal, "failed to transition to running state")
		//}

		s.logger.Printf("Successfully transitioned to Running state after starting programs")
	}

	return &c4d.RegisterResponse{
		Message: fmt.Sprintf("Node registered as %s with rank %d (session: %d)",
			nodeStatus, nodeRank, newSession.ReconnectCount),
		NodeId: req.NodeId,
	}, nil
}

func (s *C4DServer) SendHeartbeat(ctx context.Context, req *c4d.HeartbeatRequest) (*c4d.HeartbeatResponse, error) {
	s.registry.mu.Lock()
	defer s.registry.mu.Unlock()

	if health, exists := s.registry.health[req.NodeId]; exists {
		health.LastHeartbeat = time.Now()
		return &c4d.HeartbeatResponse{
			Acknowledged: true,
			Message:      "Heartbeat acknowledged",
		}, nil
	}

	return nil, status.Error(codes.NotFound, "node not found")
}

//func (s *C4DServer) NotifyFailure(ctx context.Context, req *c4d.FailureNotificationRequest) (*c4d.FailureNotificationResponse, error) {
//
//	s.handleNodeFailure(req.NodeId)
//	s.registry.mu.Lock()
//	if health, exists := s.registry.health[req.NodeId]; exists {
//		health.Status = "failed"
//		health.LastError = fmt.Errorf("%s: %s", req.FailureType, req.ErrorMessage)
//		health.ProcessID = req.ProcessId
//	}
//	s.registry.mu.Unlock()
//
//	return &c4d.FailureNotificationResponse{
//		Acknowledged: true,
//		Message:      "Failure notification processed",
//	}, nil
//}

func (s *C4DServer) NotifyFailure(ctx context.Context, req *c4d.FailureNotificationRequest) (*c4d.FailureNotificationResponse, error) {
	s.registry.mu.RLock()
	node, exists := s.registry.nodes[req.NodeId]
	if !exists || node.Status == "failed" {
		s.registry.mu.RUnlock()
		return &c4d.FailureNotificationResponse{
			Acknowledged: true,
			Message:      "Node already marked as failed or not found",
		}, nil
	}
	s.registry.mu.RUnlock()

	s.handleNodeFailure(req.NodeId)

	s.registry.mu.Lock()
	if health, exists := s.registry.health[req.NodeId]; exists {
		health.Status = "failed"
		health.LastError = fmt.Errorf("%s: %s", req.FailureType, req.ErrorMessage)
		health.ProcessID = req.ProcessId
	}
	s.registry.mu.Unlock()

	return &c4d.FailureNotificationResponse{
		Acknowledged: true,
		Message:      "Failure notification processed",
	}, nil
}

func (s *C4DServer) SendMetrics(ctx context.Context, req *c4d.MetricsRequest) (*c4d.MetricsResponse, error) {
	if s.state.Current() != Running {
		return nil, status.Error(codes.Unavailable, "server not accepting metrics")
	}

	// Get node state
	node, exists := s.registry.nodes[req.NodeId]
	if !exists {
		return nil, status.Errorf(codes.NotFound, "node %s not found", req.NodeId)
	}

	// Only process metrics from active nodes
	if node.Status == "active" {
		if err := s.metrics.ProcessMetrics(req.NodeId, req.Metrics); err != nil {
			s.logger.Printf("Error processing metrics from node %s: %v", req.NodeId, err)
			return nil, status.Errorf(codes.Internal, "failed to process metrics")
		}
	}

	return &c4d.MetricsResponse{
		Message: "Metrics processed successfully",
	}, nil
}

//func (s *C4DServer) handleNodeFailure(nodeID string) {
//	if s.state.Current() != Running {
//		s.logger.Printf("Ignoring node failure for %s - server not in Running state", nodeID)
//		return
//	}
//
//	s.logger.Printf("Handling failure of node %s", nodeID)
//
//	// Use a context with timeout for the reconfiguration
//	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
//	defer cancel()
//
//	if err := s.state.Transition(Reconfiguring); err != nil {
//		s.logger.Printf("Failed to transition to Reconfiguring state: %v", err)
//		return
//	}
//
//	success := make(chan bool, 1)
//	go func() {
//		defer func() {
//			if err := s.state.Transition(Running); err != nil {
//				s.logger.Printf("Failed to transition back to Running state: %v", err)
//			}
//		}()
//
//		// Mark node as failed
//		if err := s.registry.MarkNodeFailed(nodeID); err != nil {
//			s.logger.Printf("Error marking node as failed: %v", err)
//			success <- false
//			return
//		}
//
//		// Attempt to promote standby node if available
//		if err := s.clusterManager.promoteStandbyNode(); err != nil {
//			s.logger.Printf("Failed to promote standby node: %v", err)
//		}
//
//		// Trigger reconfiguration
//		if err := s.clusterManager.reconfigureCluster(); err != nil {
//			s.logger.Printf("Failed to reconfigure cluster: %v", err)
//			success <- false
//			return
//		}
//
//		success <- true
//	}()
//
//	select {
//	case <-ctx.Done():
//		s.logger.Printf("Node failure handling timed out")
//	case result := <-success:
//		if !result {
//			s.logger.Printf("Node failure handling failed")
//		}
//	}
//}

func (s *C4DServer) handleNodeFailure(nodeID string) {

	s.registry.mu.Lock()
	node, exists := s.registry.nodes[nodeID]
	if !exists {
		s.registry.mu.Unlock()
		s.logger.Printf("Cannot handle failure for unknown node: %s", nodeID)
		return
	}

	if err := s.state.Transition(Reconfiguring); err != nil {
		s.logger.Printf("Failed to transition to reconfiguring state: %v", err)
		return
	}

	oldStatus := node.Status
	node.Status = "failed"

	if oldStatus == "active" {
		s.metrics.metrics.activeNodes.Add(-1)
		s.metrics.metrics.failedNodes.Add(1)
	} else if oldStatus == "standby" {
		s.metrics.metrics.standbyNodes.Add(-1)
		s.metrics.metrics.failedNodes.Add(1)
	}
	s.registry.mu.Unlock()

	s.logger.Printf("Node %s marked as failed (previous status: %s)", nodeID, oldStatus)

	// only trigger reconfiguration for previously active nodes
	if oldStatus == "active" {
		go func() {
			if err := s.clusterManager.reconfigureCluster(); err != nil {
				s.logger.Printf("Failed to reconfigure cluster after node failure: %v", err)
				// If reconfiguration fails completely, transition server to failed state
				if err := s.state.Transition(Failed); err != nil {
					s.logger.Printf("Failed to transition to failed state: %v", err)
				}
			}
		}()
	}
}

func (s *C4DServer) cleanupDeadConnections() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.registry.mu.Lock()
			now := time.Now()
			nodesToRemove := make([]string, 0)

			for nodeID, health := range s.registry.health {
				if now.Sub(health.LastHeartbeat) > s.config.HeartbeatTimeout*2 {
					s.logger.Printf("Cleaning up dead connection for node: %s", nodeID)
					s.sessionManager.DisconnectSession(nodeID)
					nodesToRemove = append(nodesToRemove, nodeID)
				}
			}

			for _, nodeID := range nodesToRemove {
				if node, exists := s.registry.nodes[nodeID]; exists {
					if node.Status == "active" {
						s.metrics.metrics.activeNodes.Add(-1)
					} else if node.Status == "standby" {
						s.metrics.metrics.standbyNodes.Add(-1)
					}
				}
				delete(s.registry.nodes, nodeID)
				delete(s.registry.health, nodeID)
			}
			s.registry.mu.Unlock()
		}
	}
}
