# CCL Monitor Documentation

## Will call the following API endpoint:
- URL: `http://localhost:8081/log_ccl`
- Method: POST
- Content-Type: application/json
- Protocol: HTTP/2

## JSON Payload Examples

### 1. Collective Operation Events

#### Start of an AllReduce Operation
```json
{
    "opType": "allreduce",
    "algorithm": "ring",
    "dataType": "float32",
    "count": 1024,
    "rootRank": -1,
    "context_rank": 2,
    "context_size": 4,
    "startTime": 1703448237123456,
    "endTime": 0,
    "filename": "allreduce.cpp",
    "finished": false
}
```

#### Completion of AllReduce Operation
```json
{
    "opType": "allreduce",
    "algorithm": "ring",
    "dataType": "float32",
    "count": 1024,
    "rootRank": -1,
    "context_rank": 2,
    "context_size": 4,
    "startTime": 1703448237123456,
    "endTime": 1703448237234567,
    "filename": "allreduce.cpp",
    "finished": true
}
```

### 2. Point-to-Point Communication Events

#### Start of Send Operation
```json
{
    "opType": "send",
    "remoteRank": 3,
    "context_rank": 1,
    "context_size": 4,
    "bytes": 4096,
    "startTime": 1703448238123456,
    "endTime": 0,
    "filename": "p2p_comm.cpp",
    "finished": false
}
```

#### Completion of Send Operation
```json
{
    "opType": "send",
    "remoteRank": 3,
    "context_rank": 1,
    "context_size": 4,
    "bytes": 4096,
    "startTime": 1703448238123456,
    "endTime": 1703448238234567,
    "filename": "p2p_comm.cpp",
    "finished": true
}
```

Recieve operations are similar to send operations, but with `opType` set to `recv`.

# Process Monitoring Agent Documentation

## Overview
This API provides endpoints to manage, monitor, and control a script process. It supports process lifecycle management, environment variable configuration, and detailed system metrics collection.

Base URL: `http://localhost:8080`

## Endpoints

### Start Process
Launches a new script process with optional environment variables.

**Endpoint:** `POST /start`

**Request Body:**
```json
{
    "env_vars": {
        "key1": "value1",
        "key2": "value2"
    },
    "script_path": "path/to/script.sh"
}
```

**Parameters:**
- `env_vars` (optional): Object containing key-value pairs of environment variables
- `script_path` (optional): Custom path to the script (defaults to "launch.sh")

**Response:**
```json
{
    "current_status": "started",
    "current_pid": 1234,
    "last_execution": {
        "start_time": 1703456789,
        "end_time": null,
        "exit_code": null,
        "exit_signal": null,
        "pid": 1234,
        "env_vars": {
            "key1": "value1",
            "key2": "value2"
        },
        "script_path": "path/to/script.sh"
    }
}
```

**Error Responses:**
- `400 Bad Request`: Process already running or invalid parameters
- `500 Internal Server Error`: Failed to start process

---

### Stop Process
Stops the currently running process.

**Endpoint:** `POST /stop`

**Response:**
```json
{
    "current_status": "stopped",
    "current_pid": null,
    "last_execution": {
        "start_time": 1703456789,
        "end_time": 1703456799,
        "exit_code": 0,
        "exit_signal": null,
        "pid": 1234,
        "env_vars": {...},
        "script_path": "path/to/script.sh"
    }
}
```

**Error Responses:**
- `400 Bad Request`: No process running
- `500 Internal Server Error`: Failed to stop process

---

### Restart Process
Stops the current process and starts a new one with optional new configuration.

**Endpoint:** `POST /restart`

**Request Body:** (Same as Start Process)

**Response:** (Same as Start Process)

**Error Responses:** (Same as Start Process)

---

### Get Process Status
Retrieves the current status of the process.

**Endpoint:** `GET /status`

**Response:**
```json
{
    "current_status": "running",
    "current_pid": 1234,
    "last_execution": {
        "start_time": 1703456789,
        "end_time": null,
        "exit_code": null,
        "exit_signal": null,
        "pid": 1234,
        "env_vars": {...},
        "script_path": "path/to/script.sh"
    }
}
```

**Possible Status Values:**
- `running`: Process is currently running
- `not_running`: No process is running
- `exited`: Process has terminated
- `error`: Error occurred while checking process status

---

### Get System Metrics
Retrieves detailed system and process metrics.

**Endpoint:** `GET /metrics`

**Response:**
```json
{
    "timestamp": 1703456789,
    "metrics": {
        "system_cpu_usage": 45.2,
        "system_memory_total_bytes": 16777216000,
        "system_memory_used_bytes": 8388608000,
        "system_memory_free_bytes": 8388608000,
        "system_memory_available_bytes": 10485760000,
        "system_swap_total_bytes": 4294967296,
        "system_swap_used_bytes": 1073741824,
        "system_swap_free_bytes": 3221225472,
        "system_load_average_1m": 2.15,
        "system_load_average_5m": 1.87,
        "system_load_average_15m": 1.65,
        "system_uptime_secs": 1234567,
        "total_processes": 247,
        "cpu_cores": 8,
        "cpu_threads": 16,
        "process_metrics": {
            "pid": 1234,
            "cpu_usage": 12.5,
            "memory_bytes": 104857600,
            "virtual_memory_bytes": 209715200,
            "run_time_secs": 3600,
            "disk_read_bytes": 1048576,
            "disk_written_bytes": 524288,
            "threads": 4,
            "status": "running",
            "name": "script.sh",
            "parent_pid": 1
        }
    },
    "derived_metrics": {
        "memory_usage_percent": 50.0,
        "swap_usage_percent": 25.0,
        "memory_available_percent": 50.0
    }
}
```

**Notes:**
- `process_metrics` will be `null` if no process is running
- All memory values are in bytes
- CPU usage is in percentage (0-100)
- Time values are in seconds unless specified otherwise

## Error Response Format
All error responses follow this format:
```json
{
    "error": "Error message describing what went wrong"
}
```

## Environment Variables
Common environment variables that can be set:
- `PORT`: Server port number
- `DEBUG`: Enable debug mode
- `NODE_ENV`: Environment name
- Custom variables as needed for your script

## Limitations
1. Only one process can run at a time
2. Some system metrics might not be available on all platforms
3. Environment variables must be string key-value pairs

## Example Usage

### Starting a Process with Environment Variables
```bash
curl -X POST http://localhost:8080/start \
  -H "Content-Type: application/json" \
  -d '{
    "env_vars": {
      "DEBUG": "true",
      "PORT": "3000",
      "NODE_ENV": "development"
    }
  }'
```

### Checking Process Status
```bash
curl http://localhost:8080/status
```

### Getting System Metrics
```bash
curl http://localhost:8080/metrics
```

### Stopping the Process
```bash
curl -X POST http://localhost:8080/stop
```

### Restarting with New Configuration
```bash
curl -X POST http://localhost:8080/restart \
  -H "Content-Type: application/json" \
  -d '{
    "env_vars": {
      "DEBUG": "false",
      "PORT": "8000",
      "NODE_ENV": "production"
    }
  }'
```