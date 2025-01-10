package src

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

type APIServer struct {
	c4dServer *C4DServer
	Router    *gin.Engine
}

func NewAPIServer(c4dServer *C4DServer) *APIServer {
	api := &APIServer{
		c4dServer: c4dServer,
		Router:    gin.Default(),
	}
	api.setupRoutes()
	return api
}

func (api *APIServer) setupRoutes() {
	api.Router.GET("/health", api.handleHealth)
	api.Router.GET("/metrics", api.handleMetrics)
	api.Router.GET("/nodes", api.handleListNodes)
	api.Router.POST("/nodes/:nodeId/remove", api.handleRemoveNode)
	//api.router.POST("/config/update", api.handleConfigUpdate)
}

func (api *APIServer) handleHealth(c *gin.Context) {
	state := api.c4dServer.state.Current()
	c.JSON(http.StatusOK, gin.H{
		"status":  state.String(),
		"healthy": state != Failed,
	})
}

func (api *APIServer) handleMetrics(c *gin.Context) {
	api.c4dServer.metrics.mu.RLock()
	defer api.c4dServer.metrics.mu.RUnlock()

	activeNodes := api.c4dServer.metrics.metrics.activeNodes.Load()
	standbyNodes := api.c4dServer.metrics.metrics.standbyNodes.Load()
	failedNodes := api.c4dServer.metrics.metrics.failedNodes.Load()

	c.JSON(http.StatusOK, gin.H{
		"active_nodes":  activeNodes,
		"standby_nodes": standbyNodes,
		"failed_nodes":  failedNodes,
		"state":         api.c4dServer.state.Current().String(),
	})
}

func (api *APIServer) handleListNodes(c *gin.Context) {
	api.c4dServer.registry.mu.RLock()
	defer api.c4dServer.registry.mu.RUnlock()

	nodes := make(map[string]interface{})
	for nodeID, state := range api.c4dServer.registry.nodes {
		health := api.c4dServer.registry.health[nodeID]
		nodes[nodeID] = gin.H{
			"status":         state.Status,
			"rank":           state.Rank,
			"url":            state.NodeURL,
			"last_heartbeat": health.LastHeartbeat,
			"failure_count":  health.FailureCount,
		}
	}

	c.JSON(http.StatusOK, nodes)
}

func (api *APIServer) handleRemoveNode(c *gin.Context) {
	nodeID := c.Param("nodeId")

	// Using cluster manager instead of undefined method
	if err := api.c4dServer.clusterManager.removeNode(nodeID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Node removed successfully"})
}

//func (api *APIServer) handleConfigUpdate(c *gin.Context) {
//	var update ConfigUpdate
//	if err := c.ShouldBindJSON(&update); err != nil {
//		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
//		return
//	}
//
//	// Use configManager instead of config directly
//	configManager := NewConfigManager(api.c4dServer.config)
//	if err := configManager.UpdateConfig(update); err != nil {
//		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
//		return
//	}
//
//	c.JSON(http.StatusOK, gin.H{"message": "Configuration updated successfully"})
//}

// Add Start method to APIServer
func (api *APIServer) Start(address string) error {
	return api.Router.Run(address)
}
