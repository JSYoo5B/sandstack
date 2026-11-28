package identity

import (
	"crypto/subtle"
	"errors"
	"io"
	"math"
	"strconv"
	"sync"
	"time"

	domain "github.com/JSYoo5B/sandstack/internal/domain/identity"
)

var (
	ErrInvalidCredentials = errors.New("invalid identity credentials")
	ErrNoProjectAccess    = errors.New("user has no access to the project")
	ErrInvalidToken       = errors.New("invalid identity token")
	ErrTokenGeneration    = errors.New("identity token generation failed")
)

type TokenStore interface {
	Save(id string, issuedAt, expiresAt time.Time, userID, credentialID string)
	Lookup(id string, now time.Time) (userID, credentialID string, valid bool)
	Remove(id string)
	Clear()
}

type account struct {
	fixture  domain.Fixture
	password string
}

type Service struct {
	accounts    []account
	ttl         time.Duration
	now         func() time.Time
	generate    func() string
	store       TokenStore
	credentials ApplicationCredentialStore
	entropy     io.Reader

	mu       sync.Mutex
	sequence uint64
}

// New uses a complete fixture, a nonempty password, a positive TTL, and
// dedicated stores. The clock, generator, and entropy source must be provided.
// The generator returns an unpredictable value or an empty string if entropy is
// unavailable.
func New(
	fixture domain.Fixture,
	password string,
	ttl time.Duration,
	now func() time.Time,
	generate func() string,
	store TokenStore,
	credentials ApplicationCredentialStore,
	entropy io.Reader,
) *Service {
	return &Service{
		accounts:    []account{{fixture: fixture, password: password}},
		ttl:         ttl,
		now:         now,
		generate:    generate,
		store:       store,
		credentials: credentials,
		entropy:     entropy,
	}
}

func (s *Service) WithFixture(fixture domain.Fixture, password string) *Service {
	s.accounts = append(s.accounts, account{fixture: fixture, password: password})
	return s
}

func (s *Service) Authenticate(credentials domain.Credentials) (domain.Token, error) {
	current, found := s.accountForUser(credentials.User)
	if !found || credentials.Password == "" ||
		subtle.ConstantTimeCompare([]byte(credentials.Password), []byte(current.password)) != 1 {
		return domain.Token{}, ErrInvalidCredentials
	}
	project, found := s.project(credentials.Project)
	if !found {
		return domain.Token{}, ErrInvalidCredentials
	}
	if project.ID != current.fixture.Project.ID {
		return domain.Token{}, ErrNoProjectAccess
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.issue(current.fixture, nil)
}

func (s *Service) AuthenticateApplicationCredential(auth domain.ApplicationCredentialAuth) (domain.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var credential domain.ApplicationCredential
	var owner account
	if auth.ID != "" {
		var exists bool
		credential, exists = s.credentials.Get(auth.ID)
		if !exists {
			return domain.Token{}, ErrApplicationCredentialNotFound
		}
		if owner, exists = s.accountForUserID(credential.UserID); !exists {
			return domain.Token{}, ErrInvalidCredentials
		}
	} else {
		var found bool
		if owner, found = s.accountForUser(auth.User); !found {
			return domain.Token{}, ErrInvalidCredentials
		}
		named := s.credentials.List(owner.fixture.User.ID, auth.Name)
		if len(named) != 1 {
			return domain.Token{}, ErrInvalidCredentials
		}
		credential = named[0]
	}
	if auth.Secret == "" || !verifySecret(credential, auth.Secret) ||
		(credential.ExpiresAt != nil && !s.now().Before(*credential.ExpiresAt)) {
		return domain.Token{}, ErrInvalidCredentials
	}
	return s.issue(owner.fixture, &credential)
}

func (s *Service) issue(fixture domain.Fixture, credential *domain.ApplicationCredential) (domain.Token, error) {
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
	credentialID := ""
	if credential != nil {
		credentialID = credential.ID
		if credential.ExpiresAt != nil && credential.ExpiresAt.Before(expiresAt) {
			expiresAt = *credential.ExpiresAt
		}
	}
	s.store.Save(id, issuedAt, expiresAt, fixture.User.ID, credentialID)
	return domain.Token{
		ID:                    id,
		AuditID:               auditID,
		IssuedAt:              issuedAt,
		ExpiresAt:             expiresAt,
		Fixture:               fixture,
		ApplicationCredential: credential,
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

	current, err := s.session(token)
	return current.projectID, err
}

func (s *Service) UserForToken(token string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	current, err := s.session(token)
	return current.userID, err
}

func (s *Service) InvalidateTokens() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.store.Clear()
}

func (s *Service) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.store.Clear()
	s.credentials.Clear()
}

type session struct {
	userID     string
	projectID  string
	role       domain.Role
	credential *domain.ApplicationCredential
}

func (s session) restricted() bool {
	return s.credential != nil && s.credential.Restricted()
}

func (s *Service) session(token string) (session, error) {
	userID, credentialID, valid := s.store.Lookup(token, s.now())
	if !valid {
		return session{}, ErrInvalidToken
	}
	owner, found := s.accountForUserID(userID)
	if !found {
		s.store.Remove(token)
		return session{}, ErrInvalidToken
	}
	fixture := owner.fixture
	current := session{userID: fixture.User.ID, projectID: fixture.Project.ID, role: fixture.Role}
	if credentialID != "" {
		credential, exists := s.credentials.Get(credentialID)
		if !exists {
			s.store.Remove(token)
			return session{}, ErrInvalidToken
		}
		current.credential = &credential
	}
	return current, nil
}

// InvalidateToken removes one issued token and leaves other sessions usable.
func (s *Service) InvalidateToken(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.store.Remove(token)
}

func (s *Service) accountForUser(selector domain.Selector) (account, bool) {
	for _, candidate := range s.accounts {
		user := candidate.fixture.User
		if matches(selector, user.ID, user.Name, user.Domain) {
			return candidate, true
		}
	}
	return account{}, false
}

func (s *Service) accountForUserID(userID string) (account, bool) {
	for _, candidate := range s.accounts {
		if candidate.fixture.User.ID == userID {
			return candidate, true
		}
	}
	return account{}, false
}

func (s *Service) project(selector domain.Selector) (domain.Project, bool) {
	for _, candidate := range s.accounts {
		project := candidate.fixture.Project
		if matches(selector, project.ID, project.Name, project.Domain) {
			return project, true
		}
	}
	return domain.Project{}, false
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
