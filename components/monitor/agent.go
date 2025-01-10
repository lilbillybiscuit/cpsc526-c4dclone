package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	c4d "monitor/proto"
	"net/http"
	"strings"
	"time"
)

func (m *Monitor) updateLatencyWindow(remoteRank int, latency float64) float64 {
	m.latencyMutex.Lock()
	defer m.latencyMutex.Unlock()

	if _, exists := m.latencyWindow[remoteRank]; !exists {
		m.latencyWindow[remoteRank] = make([]float64, 0, WindowSize)
	}

	window := m.latencyWindow[remoteRank]
	window = append(window, latency)
	if len(window) > WindowSize {
		window = window[1:]
	}
	m.latencyWindow[remoteRank] = window

	var sum float64
	for _, l := range window {
		sum += l
	}
	return sum / float64(len(window))
}

func (m *Monitor) startAgent(envVars map[string]string) error {
	currentState := m.getState()
	if currentState != Initializing && currentState != Standby && currentState != Restarting {
		return ErrInvalidState
	}

	var lastErr error
	for retries := 0; retries < 5; retries++ {
		payload := map[string]interface{}{
			"env_vars": envVars,
		}

		var status AgentStatus
		if err := m.callAgentEndpoint("POST", "/start", payload, &status); err != nil {
			lastErr = err
			if strings.Contains(err.Error(), "A process is already running") {
				if err := m.callAgentEndpoint("POST", "/stop", nil, nil); err != nil {
					return err
				}
			}
			fmt.Printf("Attempt %d: Failed to start agent: %v\n", retries+1, err)
			time.Sleep(AgentRestartDelay * time.Duration(retries+1))
			continue
		}

		if status.CurrentStatus != "started" {
			lastErr = ErrAgentFailed
			continue
		}
		return nil
	}

	return fmt.Errorf("failed to start agent after retries: %v", lastErr)
}

func (m *Monitor) stopAgent() error {
	currentState := m.getState()
	if currentState != Running && currentState != Restarting {
		return ErrInvalidState
	}

	if err := m.callAgentEndpoint("POST", "/stop", nil, nil); err != nil {
		return fmt.Errorf("failed to stop agent: %w", err)
	}

	return nil
}

func (m *Monitor) restartAgent(envVars map[string]string) error {
	payload := map[string]interface{}{
		"env_vars": envVars,
	}

	var agentStatus AgentStatus
	if err := m.callAgentEndpoint("POST", "/restart", payload, &agentStatus); err != nil {
		return fmt.Errorf("failed to restart agent: %w", err)
	}

	fmt.Printf("Agent restarted with PID %d\n", agentStatus.CurrentPID)
	return nil
}

func (m *Monitor) callAgentEndpoint(method, endpoint string, payload interface{}, result interface{}) error {
	url := fmt.Sprintf("%s%s", AgentBaseURL, endpoint)

	var body io.Reader
	if payload != nil {
		jsonData, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %w", err)
		}
		body = bytes.NewReader(jsonData)
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		println(m.nodeURL + endpoint + " failed")
		return fmt.Errorf("agent returned error status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

func (m *Monitor) sendMetrics() {
	ticker := time.NewTicker(MetricsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			return
		case <-ticker.C:
			if m.getState() != Running {
				return
			}

			if err := m.sendMetricsToServer(); err != nil {
				fmt.Printf("Failed to send heartbeat: %v\n", err)
			}

			var status AgentStatus
			if err := m.callAgentEndpoint("GET", "/status", nil, &status); err != nil {
				fmt.Printf("Failed to get agent status: %v\n", err)
				m.setState(Failed)
				return
			}

			if status.LastExecution.ExitCode != nil && *status.LastExecution.ExitCode != 0 {
				m.setState(Failed)
				return
			}

			if err := m.sendMetricsToServer(); err != nil {
				fmt.Printf("Failed to send metrics: %v\n", err)
			}
		}
	}
}

func (m *Monitor) sendMetricsToServer() error {
	if m.getState() != Running {
		return ErrInvalidState
	}

	var metrics AgentMetrics
	if err := m.callAgentEndpoint("GET", "/metrics", nil, &metrics); err != nil {
		return fmt.Errorf("failed to fetch metrics: %w", err)
	}

	// Update local metrics
	if metrics.Metrics.ProcessMetrics != nil {
		m.cpuUsage = metrics.Metrics.ProcessMetrics.CPUUsage
		m.ramUsage = float64(metrics.Metrics.ProcessMetrics.MemoryBytes)
	}

	// Calculate average latencies
	m.latencyMutex.RLock()
	avgLatencies := make(map[int32]float64)
	for remoteRank, window := range m.latencyWindow {
		if len(window) > 0 {
			var sum float64
			for _, l := range window {
				sum += l
			}
			avgLatencies[int32(remoteRank)] = sum / float64(len(window))
		}
	}
	m.latencyMutex.RUnlock()

	// Send metrics to server
	ctx, cancel := context.WithTimeout(context.Background(), MetricsTimeout)
	defer cancel()

	req := &c4d.MetricsRequest{
		NodeId: m.nodeID,
		Metrics: &c4d.Metrics{
			RamUsage: m.ramUsage,
			CpuUsage: m.cpuUsage,
			RankLatencies: &c4d.RankLatencies{
				Rank:         int32(m.rank),
				AvgLatencies: avgLatencies,
			},
		},
	}

	_, err := m.grpcClient.SendMetrics(ctx, req)
	return err
}
