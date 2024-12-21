package main

import (
    "bytes"
    "encoding/binary"
    "encoding/json"
    "fmt"
    "log"
    "net/http"
    "sync"
)

type FailureServer struct {
    agents map[string]string // taskID -> agent URL
    mu     sync.RWMutex
}

type AgentUpdateRequest struct {
    ShouldBeRunning bool  `json:"shouldBeRunning"`
    NetworkDelay    int32 `json:"networkDelay"`
}

func NewFailureServer() *FailureServer {
    return &FailureServer{
        agents: make(map[string]string),
    }
}

func (s *FailureServer) registerAgent(taskID, url string) {
    s.mu.Lock()
    defer s.mu.Unlock()
    log.Printf("Registering agent %s at %s", taskID, url)
    s.agents[taskID] = url
}

func (s *FailureServer) getAgentURL(taskID string) (string, bool) {
    s.mu.RLock()
    defer s.mu.RUnlock()
    url, exists := s.agents[taskID]
    return url, exists
}

func (s *FailureServer) listAgents() []string {
    s.mu.RLock()
    defer s.mu.RUnlock()
    agents := make([]string, 0, len(s.agents))
    for taskID := range s.agents {
        agents = append(agents, taskID)
    }
    return agents
}

func main() {
    server := NewFailureServer()

    // Register a new agent
    http.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
            http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
            return
        }

        var data struct {
            TaskID string `json:"taskId"`
            URL    string `json:"url"`
        }

        if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
            http.Error(w, "Invalid request body", http.StatusBadRequest)
            return
        }

        server.registerAgent(data.TaskID, data.URL)
        w.WriteHeader(http.StatusOK)
    })

    // List all registered agents
    http.HandleFunc("/agents", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
            http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
            return
        }

        agents := server.listAgents()
        json.NewEncoder(w).Encode(map[string][]string{"agents": agents})
    })

    // Update agent state
    http.HandleFunc("/agent/", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
            http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
            return
        }

        taskID := r.URL.Path[len("/agent/"):]
        agentURL, exists := server.getAgentURL(taskID)
        if !exists {
            http.Error(w, "Agent not found", http.StatusNotFound)
            return
        }

        var updateReq AgentUpdateRequest
        if err := json.NewDecoder(r.Body).Decode(&updateReq); err != nil {
            http.Error(w, "Invalid request body", http.StatusBadRequest)
            return
        }

        // Create binary request for agent
        data := make([]byte, 5)
        if updateReq.ShouldBeRunning {
            data[0] = 1
        }
        binary.BigEndian.PutUint32(data[1:], uint32(updateReq.NetworkDelay))

        // Forward request to agent
        resp, err := http.Post(fmt.Sprintf("%s/update", agentURL), "application/octet-stream", bytes.NewReader(data))
        if err != nil {
            http.Error(w, "Failed to update agent", http.StatusInternalServerError)
            return
        }
        defer resp.Body.Close()

        if resp.StatusCode != http.StatusOK {
            http.Error(w, "Agent update failed", http.StatusInternalServerError)
            return
        }

        w.WriteHeader(http.StatusOK)
    })

    // Get agent state
    http.HandleFunc("/agent/state/", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
            http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
            return
        }

        taskID := r.URL.Path[len("/agent/state/"):]
        agentURL, exists := server.getAgentURL(taskID)
        if !exists {
            http.Error(w, "Agent not found", http.StatusNotFound)
            return
        }

        resp, err := http.Get(fmt.Sprintf("%s/state", agentURL))
        if err != nil {
            http.Error(w, "Failed to get agent state", http.StatusInternalServerError)
            return
        }
        defer resp.Body.Close()

        var data [5]byte
        if _, err := resp.Body.Read(data[:]); err != nil {
            http.Error(w, "Failed to read agent state", http.StatusInternalServerError)
            return
        }

        response := AgentUpdateRequest{
            ShouldBeRunning: data[0] == 1,
            NetworkDelay:    int32(binary.BigEndian.Uint32(data[1:])),
        }

        json.NewEncoder(w).Encode(response)
    })

    log.Println("Failure server starting on port 8092")
    if err := http.ListenAndServe(":8092", nil); err != nil {
        log.Fatal(err)
    }
}
