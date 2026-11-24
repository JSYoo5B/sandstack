package identity

import (
	"sync"
	"time"
)

// Sweep opportunistically during writes so tokens that are never used again
// are dropped without a background goroutine or a scan on each issuance.
const sweepInterval = time.Minute

type Tokens struct {
	mu        sync.Mutex
	expiresAt map[string]time.Time
	nextSweep time.Time
}

func New() *Tokens {
	return &Tokens{expiresAt: make(map[string]time.Time)}
}

func (t *Tokens) Save(id string, issuedAt, expiresAt time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !issuedAt.Before(t.nextSweep) {
		for token, expiry := range t.expiresAt {
			if !issuedAt.Before(expiry) {
				delete(t.expiresAt, token)
			}
		}
		t.nextSweep = issuedAt.Add(sweepInterval)
	}
	t.expiresAt[id] = expiresAt
}

func (t *Tokens) Valid(id string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	expiry, exists := t.expiresAt[id]
	if exists && !now.Before(expiry) {
		delete(t.expiresAt, id)
		return false
	}
	return exists
}

func (t *Tokens) Remove(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	delete(t.expiresAt, id)
}

func (t *Tokens) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.expiresAt = make(map[string]time.Time)
	t.nextSweep = time.Time{}
}
