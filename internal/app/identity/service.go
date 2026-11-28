package identity

import (
	"crypto/subtle"
	"errors"
	"math"
	"strconv"
	"sync"
	"time"

	domain "github.com/JSYoo5B/sandstack/internal/domain/identity"
)

var (
	ErrInvalidCredentials = errors.New("invalid identity credentials")
	ErrInvalidToken       = errors.New("invalid identity token")
	ErrTokenGeneration    = errors.New("identity token generation failed")
)

type TokenStore interface {
	Save(id string, issuedAt, expiresAt time.Time)
	Valid(id string, now time.Time) bool
	Remove(id string)
	Clear()
}

type Service struct {
	fixture  domain.Fixture
	password string
	ttl      time.Duration
	now      func() time.Time
	generate func() string
	store    TokenStore

	mu       sync.Mutex
	sequence uint64
}

// New uses a complete fixture, a nonempty password, a positive TTL, and a
// dedicated store. The clock and generator must be provided. The generator
// returns an unpredictable value or an empty string if entropy is unavailable.
func New(
	fixture domain.Fixture,
	password string,
	ttl time.Duration,
	now func() time.Time,
	generate func() string,
	store TokenStore,
) *Service {
	return &Service{
		fixture:  fixture,
		password: password,
		ttl:      ttl,
		now:      now,
		generate: generate,
		store:    store,
	}
}

func (s *Service) Authenticate(credentials domain.Credentials) (domain.Token, error) {
	user, project := s.fixture.User, s.fixture.Project
	if credentials.Password == "" ||
		subtle.ConstantTimeCompare([]byte(credentials.Password), []byte(s.password)) != 1 ||
		!matches(credentials.User, user.ID, user.Name, user.Domain) ||
		!matches(credentials.Project, project.ID, project.Name, project.Domain) {
		return domain.Token{}, ErrInvalidCredentials
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sequence == math.MaxUint64 {
		return domain.Token{}, ErrTokenGeneration
	}
	entropy, auditID := s.generate(), s.generate()
	if entropy == "" || auditID == "" {
		return domain.Token{}, ErrTokenGeneration
	}

	// Keep issued IDs distinct even after a token is invalidated and removed.
	s.sequence++
	id := entropy + "." + strconv.FormatUint(s.sequence, 10)
	issuedAt := s.now()
	expiresAt := issuedAt.Add(s.ttl)
	s.store.Save(id, issuedAt, expiresAt)
	return domain.Token{
		ID:        id,
		AuditID:   auditID,
		IssuedAt:  issuedAt,
		ExpiresAt: expiresAt,
		Fixture:   s.fixture,
	}, nil
}

func (s *Service) Validate(token string) error {
	_, err := s.ProjectForToken(token)
	return err
}

// ProjectForToken returns the project bound to a live token.
func (s *Service) ProjectForToken(token string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.store.Valid(token, s.now()) {
		return "", ErrInvalidToken
	}
	return s.fixture.Project.ID, nil
}

func (s *Service) UserForToken(token string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.store.Valid(token, s.now()) {
		return "", ErrInvalidToken
	}
	return s.fixture.User.ID, nil
}

func (s *Service) InvalidateTokens() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.store.Clear()
}

// InvalidateToken removes one issued token and leaves other sessions usable.
func (s *Service) InvalidateToken(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.store.Remove(token)
}

func matches(selector domain.Selector, id, name string, namespace domain.Domain) bool {
	if selector.ID != "" {
		return selector.ID == id && selector.Name == "" &&
			selector.DomainID == "" && selector.DomainName == ""
	}
	if selector.Name == "" || selector.Name != name {
		return false
	}
	if selector.DomainID != "" {
		return selector.DomainID == namespace.ID && selector.DomainName == ""
	}
	return selector.DomainName != "" && selector.DomainName == namespace.Name
}
