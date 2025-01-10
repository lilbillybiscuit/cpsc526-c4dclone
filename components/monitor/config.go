package main

import "time"

const (
	MetricsInterval = time.Second

	MetricsTimeout      = 5 * time.Second
	RegistrationTimeout = 15 * time.Second
	ShutdownTimeout     = 5 * time.Second

	InitialBackoff = 2 * time.Second
	MaxBackoff     = 5 * time.Second
	MaxRetries     = 10

	GRPCKeepAliveTime    = 10 * time.Second
	GRPCKeepAliveTimeout = 5 * time.Second

	AgentRestartDelay = time.Second

	HeartbeatInterval = 2 * time.Second
	HeartbeatTimeout  = 5 * time.Second
)

const (
	ServerBaseURL = "c4d-server.central-services:8091"
	WindowSize    = 100
)

var (
	AgentBaseURL = "http://localhost:8090"
)

const GRPCServiceConfig = `{
	"methodConfig": [{
		"name": [{"service": ""}],
		"waitForReady": true,
		"retryPolicy": {
			"maxAttempts": 5,
			"initialBackoff": "0.1s",
			"maxBackoff": "5s",
			"backoffMultiplier": 2.0,
			"retryableStatusCodes": [
				"UNAVAILABLE",
				"INTERNAL",
				"DEADLINE_EXCEEDED"
			]
		}
	}]
}`
