package identity

import (
	"slices"
	"sync"

	domain "github.com/JSYoo5B/sandstack/internal/domain/identity"
)

type ApplicationCredentials struct {
	mu          sync.Mutex
	credentials []domain.ApplicationCredential
}

func NewApplicationCredentials() *ApplicationCredentials {
	return &ApplicationCredentials{}
}

func (a *ApplicationCredentials) Add(credential domain.ApplicationCredential) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	if slices.ContainsFunc(a.credentials, func(existing domain.ApplicationCredential) bool {
		return existing.ID == credential.ID ||
			(existing.UserID == credential.UserID && existing.Name == credential.Name)
	}) {
		return false
	}
	a.credentials = append(a.credentials, credential)
	return true
}

func (a *ApplicationCredentials) Get(id string) (domain.ApplicationCredential, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	i := slices.IndexFunc(a.credentials, func(c domain.ApplicationCredential) bool { return c.ID == id })
	if i < 0 {
		return domain.ApplicationCredential{}, false
	}
	return a.credentials[i], true
}

func (a *ApplicationCredentials) List(userID, name string) []domain.ApplicationCredential {
	a.mu.Lock()
	defer a.mu.Unlock()

	var matches []domain.ApplicationCredential
	for _, credential := range a.credentials {
		if credential.UserID == userID && (name == "" || credential.Name == name) {
			matches = append(matches, credential)
		}
	}
	return matches
}

func (a *ApplicationCredentials) Remove(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	before := len(a.credentials)
	a.credentials = slices.DeleteFunc(a.credentials, func(c domain.ApplicationCredential) bool { return c.ID == id })
	return len(a.credentials) != before
}

func (a *ApplicationCredentials) Clear() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.credentials = nil
}
