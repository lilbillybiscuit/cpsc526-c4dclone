package main

import (
    "encoding/binary"
    "log"
    "net/http"
    "os"
    "sync"
    "fmt"
    "time"
    "bytes"
    "encoding/json"
)

type AgentState struct {
    shouldBeRunning bool
    networkDelay    int32
    mu             sync.RWMutex
}

func NewAgentState() *AgentState {
    return &AgentState{
        shouldBeRunning: true,
        networkDelay:    0,
    }
}

func (s *AgentState) SetState(running bool, delay int32) {
    s.mu.Lock()
    defer s.mu.Unlock()
    
    s.shouldBeRunning = running
    if delay > 1048576 { // 2^20
        delay = 1048576
    }
    s.networkDelay = delay
}

func (s *AgentState) GetStateBinary() []byte {
    s.mu.RLock()
    defer s.mu.RUnlock()
    
    response := make([]byte, 5)
    if s.shouldBeRunning {
        response[0] = 1
    }
    binary.BigEndian.PutUint32(response[1:], uint32(s.networkDelay))
    return response
}

func registerWithServer(taskID string, agentPort int, serverURL string) error {
    // Registration data
    data := map[string]string{
        "taskId": taskID,
        "url": fmt.Sprintf("http://%s:%d", os.Getenv("HOSTNAME"), agentPort),
    }
    
    jsonData, err := json.Marshal(data)
    if err != nil {
        return err
    }

    // Keep trying to register until successful
    for {
        resp, err := http.Post(
            fmt.Sprintf("%s/register", serverURL),
            "application/json",
            bytes.NewBuffer(jsonData),
        )
        
        if err == nil && resp.StatusCode == http.StatusOK {
            resp.Body.Close()
            log.Printf("Successfully registered with failure server")
            return nil
        }
        
        if err != nil {
            log.Printf("Failed to register with failure server: %v", err)
        } else {
            resp.Body.Close()
            log.Printf("Failed to register with failure server. Status: %d", resp.StatusCode)
        }
        
        time.Sleep(5 * time.Second)
    }
}

func main() {
    taskID := os.Getenv("TASK_ID")
    if taskID == "" {
        log.Fatal("TASK_ID environment variable not set")
    }
    
    serverURL := os.Getenv("FAILURE_SERVER_URL")
    if serverURL == "" {
        serverURL = "http://failure-server:8092"
    }
    
    agentPort := 8082
    state := NewAgentState()
    
    // Start registration process in background
    go func() {
        if err := registerWithServer(taskID, agentPort, serverURL); err != nil {
            log.Printf("Failed to register with server: %v", err)
        }
    }()
    
    http.HandleFunc("/state", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
            http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
            return
        }
        
        w.Header().Set("Content-Type", "application/octet-stream")
        w.Write(state.GetStateBinary())
    })
    
    http.HandleFunc("/update", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
            http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
            return
        }
        
        var data [5]byte
        if _, err := r.Body.Read(data[:]); err != nil {
            http.Error(w, "Invalid request body", http.StatusBadRequest)
            return
        }
        
        running := data[0] == 1
        delay := int32(binary.BigEndian.Uint32(data[1:]))
        
        state.SetState(running, delay)
        w.WriteHeader(http.StatusOK)
    })
    
    log.Printf("Failure agent starting for task: %s on port %d", taskID, agentPort)
    if err := http.ListenAndServe(fmt.Sprintf(":%d", agentPort), nil); err != nil {
        log.Fatal(err)
    }
}