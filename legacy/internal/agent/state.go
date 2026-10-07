package agent

import (
	"fmt"
	"sync"
)

// stateMachine manages valid AgentState transitions.
type stateMachine struct {
	mu      sync.Mutex
	current AgentState
}

// validTransitions defines allowed state movements.
var validTransitions = map[AgentState][]AgentState{
	AgentPending:   {AgentRunning},
	AgentRunning:   {AgentSuspended, AgentCompleted, AgentFailed},
	AgentSuspended: {AgentRunning, AgentFailed},
	AgentCompleted: {},
	AgentFailed:    {},
}

func newStateMachine(initial AgentState) *stateMachine {
	return &stateMachine{current: initial}
}

// Transition moves to the next state if the transition is valid.
func (sm *stateMachine) Transition(next AgentState) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	allowed := validTransitions[sm.current]
	for _, a := range allowed {
		if a == next {
			sm.current = next
			return nil
		}
	}
	return fmt.Errorf("invalid transition %s -> %s", sm.current, next)
}

// Current returns the current state (thread-safe).
func (sm *stateMachine) Current() AgentState {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.current
}
