package identity

import (
	"sync"
	"time"
)

// Sweep opportunistically during writes so tokens that are never used again
// are dropped without a background goroutine or a scan on each issuance.
const sweepInterval = time.Minute

type token struct {
	expiresAt    time.Time
	userID       string
	credentialID string
}

type Tokens struct {
	mu        sync.Mutex
	tokens    map[string]token
	nextSweep time.Time
}

func New() *Tokens {
	return &Tokens{tokens: make(map[string]token)}
}

func (t *Tokens) Save(id string, issuedAt, expiresAt time.Time, userID, credentialID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !issuedAt.Before(t.nextSweep) {
		for issued, record := range t.tokens {
			if !issuedAt.Before(record.expiresAt) {
				delete(t.tokens, issued)
			}
		}
		t.nextSweep = issuedAt.Add(sweepInterval)
	}
	t.tokens[id] = token{expiresAt: expiresAt, userID: userID, credentialID: credentialID}
}

func (t *Tokens) Lookup(id string, now time.Time) (userID, credentialID string, valid bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	record, exists := t.tokens[id]
	if exists && !now.Before(record.expiresAt) {
		delete(t.tokens, id)
		return "", "", false
	}
	return record.userID, record.credentialID, exists
}

func (t *Tokens) Remove(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	delete(t.tokens, id)
}

func (t *Tokens) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.tokens = make(map[string]token)
	t.nextSweep = time.Time{}
}
