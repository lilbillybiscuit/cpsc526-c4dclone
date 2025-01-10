package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"
)

func main() {
	monitor, err := NewMonitor()
	if err != nil {
		fmt.Printf("Failed to create monitor: %v\n", err)
		os.Exit(1)
	}

	// Start monitor
	if err := monitor.Start(); err != nil {
		fmt.Printf("Failed to start monitor: %v\n", err)
		os.Exit(1)
	}

	// Setup HTTP server
	r := gin.New()
	setupRoutes(r, monitor)

	// Setup graceful shutdown
	setupGracefulShutdown(monitor)

	// Start HTTP server
	startHTTPServer(r)
}

func setupRoutes(r *gin.Engine, monitor *Monitor) {
	r.POST("/log_ccl", monitor.handleLogCCL)
	r.POST("/restart", monitor.handleRestart)
	//r.POST("/activate", monitor.handleActivate)
	r.POST("/remove", monitor.handleRemove)
	r.GET("/metrics", monitor.handleMetrics)
	r.GET("/state", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"state": monitor.getState().String()})
	})
	r.POST("/command", monitor.handleCommand)
}

func setupGracefulShutdown(monitor *Monitor) {
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		<-sigCh

		fmt.Println("Shutting down...")
		if monitor.getState() == Running {
			if err := monitor.stopAgent(); err != nil {
				fmt.Printf("Failed to stop agent: %v\n", err)
			}
		}
		monitor.setState(Failed)
		os.Exit(0)
	}()
}

func startHTTPServer(r *gin.Engine) {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	agent_port := os.Getenv("AGENT_PORT")
	if agent_port != "" {
		AgentBaseURL = "http://localhost:" + agent_port
		// otherwise is 8090
	}

	if err := r.Run(":" + port); err != nil {
		fmt.Printf("Server failed: %v\n", err)
		os.Exit(1)
	}
}
