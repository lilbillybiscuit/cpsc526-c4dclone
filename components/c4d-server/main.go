package main

import (
	"c4d-server/src"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// Command line flags for all configuration
	expectedNodes := flag.Int("expected-nodes", 3, "Total number of expected nodes")
	standbyNodes := flag.Int("standby-nodes", 1, "Number of standby nodes")
	apiPort := flag.String("api-port", "8080", "API server port")
	grpcPort := flag.String("grpc-port", "8091", "gRPC server port")
	heartbeatTimeout := flag.Duration("heartbeat-timeout", 5*time.Second, "Node heartbeat timeout")
	latencyThreshold := flag.Duration("latency-threshold", 100*time.Millisecond, "Latency threshold for node health")
	flag.Parse()

	// Validate arguments
	activeNodes := *expectedNodes - *standbyNodes
	if activeNodes <= 0 {
		log.Fatalf("Invalid configuration: standby nodes (%d) must be less than expected nodes (%d)",
			*standbyNodes, *expectedNodes)
	}

	// Initialize logger
	logger := log.New(os.Stdout, "[C4D] ", log.LstdFlags)

	// Create configuration from command line arguments
	config := &src.ServerConfig{
		ExpectedNodes:    *expectedNodes,
		StandbyNodes:     *standbyNodes,
		ActiveNodes:      activeNodes,
		HeartbeatTimeout: *heartbeatTimeout,
		LatencyThreshold: *latencyThreshold,
		GRPCPort:         *grpcPort,
		APIPort:          *apiPort,
	}

	// Create and start C4D server
	server := src.NewC4DServer(config)

	// Create API server
	apiServer := src.NewAPIServer(server)

	// Start servers in goroutines
	errChan := make(chan error, 2)

	// Start gRPC server
	go func() {
		if err := server.Start(); err != nil {
			errChan <- fmt.Errorf("gRPC server error: %v", err)
		}
	}()

	// Start API server
	go func() {
		if err := apiServer.Router.Run(":" + config.APIPort); err != nil {
			errChan <- fmt.Errorf("API server error: %v", err)
		}
	}()

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal or error
	select {
	case err := <-errChan:
		logger.Printf("Server error: %v", err)
	case sig := <-sigChan:
		logger.Printf("Received signal: %v", sig)
	}
	
	server.Stop()
	logger.Println("Shutdown complete")
}
