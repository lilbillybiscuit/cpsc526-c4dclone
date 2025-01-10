package src

import "time"

const (
	// Server configuration
	DefaultGRPCPort   = "8091"
	DefaultAPIPort    = "8080"
	DefaultConfigFile = "config.json"

	// Timeouts and intervals
	DefaultHeartbeatTimeout = 5 * time.Second
	DefaultLatencyThreshold = 100 * time.Millisecond
	DefaultRetryInterval    = 1 * time.Second
	MaxRetryAttempts        = 3

	// Metrics
	MetricsBufferSize = 1000
	LatencyWindowSize = 100

	// State management
	MaxStateTransitionRetries = 3
	StateTransitionTimeout    = 5 * time.Second

	NodeRecoveryTimeout = 30 * time.Second

	// Health monitoring
	HeartbeatTimeout = 5 * time.Second
)

// Response messages
const (
	MsgServerStarting       = "Server starting"
	MsgServerStopping       = "Server stopping"
	MsgNodeRegistered       = "Node registered successfully"
	MsgNodeRemoved          = "Node removed successfully"
	MsgConfigurationUpdated = "Configuration updated successfully"
	MsgInvalidRequest       = "Invalid request"
	MsgUnauthorized         = "Unauthorized"
	MsgInternalError        = "Internal server error"
)
