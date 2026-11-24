package sandbox

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStateHoldsSharedAndExclusiveLocksDuringCallbacks(t *testing.T) {
	var state *State
	var resets []string
	state = NewState(
		func() {
			if state.mu.TryRLock() {
				state.mu.RUnlock()
				t.Error("reset callback allowed an operation to acquire the shared lock")
			}
			resets = append(resets, "first")
		},
		func() { resets = append(resets, "second") },
	)

	state.Run(func() {
		if state.mu.TryLock() {
			state.mu.Unlock()
			t.Error("operation allowed reset to acquire the exclusive lock")
		}
		if assert.True(t, state.mu.TryRLock()) {
			state.mu.RUnlock()
		}
	})
	state.Reset()

	assert.Equal(t, []string{"first", "second"}, resets)
}
