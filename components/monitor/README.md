# monitor

Description of monitor component.


### Compile protobuf:

```bash
# install protoc
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative c4d.proto
```

Here's the documentation for both client and server components:

# C4D Monitoring System Documentation

## Overview
The C4D Monitoring System consists of two main components:
1. Client (Monitor) - Runs on each compute node
2. Server - Central coordination and monitoring service

## Client (Monitor)

### Purpose
- Monitors local compute node status and performance
- Tracks inter-node communication latencies
- Reports metrics to central server
- Handles rank reassignment and computation restarts

### Key Components

#### State Management
```go
type Monitor struct {
    // Communication channels
    stopEvent    chan bool          // Signals shutdown
    grpcClient   c4d.C4DServiceClient
    
    // Node status
    rank         int               // Current compute rank
    worldSize    int               // Total active nodes
    nodeStatus   string            // "active" or "standby"
    isStandby    bool             // Standby status
    
    // Metrics
    ramUsage     float64          // Current RAM usage
    cpuUsage     float64          // Current CPU usage
    latencyWindow map[int][]float64 // Sliding window of P2P latencies per rank
    windowSize    int              // Size of latency sliding window
    
    // Configuration
    nodeURL      string           // This node's URL
    distEnvVars  map[string]string // Distributed training environment variables
    activeNodes  map[string]NodeInfo // Information about all active nodes
}
```

### Key Functionalities

1. **Latency Tracking**
    - Maintains sliding window of P2P communication latencies
    - Computes running averages before sending to server
    - Only tracks send/recv operation latencies

2. **Metric Reporting**
    - Reports essential metrics to server:
        - CPU/RAM usage
        - Averaged P2P latencies
        - Process status
    - Frequency: Every second

3. **Configuration Management**
    - Handles rank reassignment
    - Updates distributed training environment
    - Manages node status transitions

4. **HTTP Endpoints**
   ```
   POST /log_ccl     - Receive CCL operation logs
   POST /activate    - Activate standby node
   POST /remove      - Remove node from computation
   GET /metrics      - Get current node metrics
   POST /restart     - Handle computation restart
   ```

## Server

### Purpose
- Coordinates compute nodes
- Monitors node health
- Detects and handles node failures
- Manages rank assignments

### Key Components

#### State Management
```go
type C4DServer struct {
    nodes             map[string]*NodeState // Active and standby nodes
    latencyMatrix     *LatencyMatrix       // P2P communication latencies
    standbyNodes      []string             // Available standby nodes
    worldSize         int                  // Current world size
    keepAliveInterval time.Duration        // Node health check interval
    latencyThreshold  time.Duration        // High latency threshold
}
```

### Key Functionalities

1. **Node Health Monitoring**
    - Tracks node heartbeats
    - Detects node failures (missing heartbeats)
    - Monitors process status
    - Check interval: 5 seconds

2. **Latency Analysis**
    - Maintains global latency matrix
    - Uses pre-averaged latencies from clients
    - Detects problematic communication patterns
    - Uses statistical analysis (mean + 2*stddev threshold)

3. **Failure Handling**
   ```
   Triggers:
   - Node exceeds keepalive interval
   - Process failure reported
   - Consistent high latency detected
   
   Actions:
   1. Mark node as failed
   2. Select standby node
   3. Reassign ranks
   4. Notify all nodes of new configuration
   5. Trigger computation restart
   ```

4. **Configuration Management**
    - Manages rank assignments
    - Maintains active/standby node lists
    - Ensures configuration consistency
    - Coordinates computation restarts

### Communication Protocol

#### Client → Server
```protobuf
message MetricsRequest {
    string node_id = 1;
    Metrics metrics = 2;
    NodeStatus status = 3;
}

message Metrics {
    double ram_usage = 1;
    double cpu_usage = 2;
    RankLatencies rank_latencies = 3;
}

message NodeStatus {
    bool is_alive = 1;
    int64 process_id = 2;
}
```

#### Server → Client
```protobuf
message RestartConfig {
    int32 new_rank = 1;
    int32 world_size = 2;
    repeated NodeInfo active_nodes = 3;
}
```

### Failure Detection and Recovery

1. **Detection Methods**
    - Missed heartbeats (>5 seconds)
    - Process failure reports
    - Statistical latency analysis

2. **Recovery Process**
   ```
   1. Identify failure
   2. Select standby node
   3. Update configuration
   4. Notify all nodes
   5. Wait for acknowledgments
   6. Resume computation
   ```

### Performance Considerations

1. **Client-side**
    - Local latency averaging reduces server load
    - Minimal metric reporting (essential data only)
    - Efficient P2P latency tracking

2. **Server-side**
    - No log storage
    - Real-time metric processing
    - Efficient rank reassignment
    - Statistical analysis for failure detection

### Configuration Parameters

```go
const (
    keepAliveInterval = 5 * time.Second
    latencyWindowSize = 100
    metricsInterval  = 1 * time.Second
    maxRetries      = 3
)
```