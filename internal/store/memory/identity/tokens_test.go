package identity

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSaveSweepsExpiredTokensWithoutEvictingActiveTokens(t *testing.T) {
	store := New()
	start := time.Date(2026, time.November, 25, 0, 0, 0, 0, time.UTC)
	store.Save("expired", start, start.Add(time.Second))
	store.Save("active", start, start.Add(time.Hour))

	current := start.Add(sweepInterval)
	store.Save("new", current, current.Add(time.Hour))

	assert.NotContains(t, store.expiresAt, "expired")
	assert.True(t, store.Valid("active", current))
	assert.True(t, store.Valid("new", current))
}
