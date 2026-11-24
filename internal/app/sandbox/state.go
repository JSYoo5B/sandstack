package sandbox

import (
	"slices"
	"sync"
)

// State coordinates fixture operations with a reset across all services.
type State struct {
	mu        sync.RWMutex
	resetters []func()
}

func NewState(resetters ...func()) *State {
	return &State{resetters: slices.Clone(resetters)}
}

// Run keeps one complete operation on one side of any reset.
// The callback must not call Run or Reset on this State.
func (s *State) Run(fn func()) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	fn()
}

// Reset restores the services in registration order while operations wait.
// Resetters must not call Run or Reset on this State.
func (s *State) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, reset := range s.resetters {
		reset()
	}
}
