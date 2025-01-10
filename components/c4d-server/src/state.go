package src

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"time"
)

type ServerState struct {
	current         State
	lastTransition  time.Time
	transitionCount map[State]int
	mu              MonitoredRWMutex
}

func NewServerState() *ServerState {
	return &ServerState{
		current:         Initializing,
		lastTransition:  time.Now(),
		transitionCount: make(map[State]int),
	}
}

func (s *ServerState) Transition(to State) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	//stackBuf := make([]byte, 1024)
	//stackSize := runtime.Stack(stackBuf, false)
	//stackTrace := string(stackBuf[:stackSize])

	if to == Failed {
		if s.current != Failed { // Only log if not already failed
			log.Printf("State transition: %v -> Failed (at %s)",
				s.current, time.Now().Format(time.RFC3339))
			s.current = Failed
			s.lastTransition = time.Now()
			s.transitionCount[Failed]++

			// Trigger server shutdown
			log.Printf("Server entering failed state - shutting down")
			os.Exit(1)
		}
		return nil
	}

	// Define valid state transitions
	validTransitions := map[State][]State{
		Initializing:  {Running},                        // Can only go to Running
		Running:       {Reconfiguring, Failed},          // Can go to Reconfiguring
		Reconfiguring: {Running, Reconfiguring, Failed}, // Running, or stay in Reconfiguring
		Failed:        {},                               // Terminal state
	}

	_, file, line, ok := runtime.Caller(1)
	if !ok {
		file = "unknown"
		line = 0
	}

	if valid, ok := validTransitions[s.current]; ok {
		for _, allowedState := range valid {
			if allowedState == to {
				//log.Println("State transition:", s.current, "->", to, "(at", time.Now().Format(time.RFC3339), ")")
				log.Printf("State transition: %v -> %v (at %s) (called from %s:%d)\n",
					s.current, to, time.Now().Format(time.RFC3339), file, line)
				s.current = to
				s.lastTransition = time.Now()
				s.transitionCount[to]++
				return nil
			}
		}
	}

	return fmt.Errorf("invalid state transition: %v -> %v (called from %s:%d)\n",
		s.current, to, file, line)
}

func (s *ServerState) Current() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}

func (s *ServerState) LastTransitionTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastTransition
}
