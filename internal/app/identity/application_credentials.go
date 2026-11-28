package identity

import (
	"crypto/pbkdf2"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"slices"

	domain "github.com/JSYoo5B/sandstack/internal/domain/identity"
)

const (
	secretBytes      = 64
	secretSaltBytes  = 16
	secretHashRounds = 25000
	secretHashBytes  = sha512.Size
)

var (
	ErrForbidden                       = errors.New("identity action is forbidden")
	ErrApplicationCredentialNotFound   = errors.New("application credential not found")
	ErrApplicationCredentialExists     = errors.New("application credential name already exists")
	ErrInvalidApplicationCredential    = errors.New("invalid application credential")
	ErrRoleNotFound                    = errors.New("role not found")
	ErrApplicationCredentialGeneration = errors.New("application credential generation failed")
)

type ApplicationCredentialStore interface {
	Add(credential domain.ApplicationCredential) bool
	Get(id string) (domain.ApplicationCredential, bool)
	List(userID, name string) []domain.ApplicationCredential
	Remove(id string) bool
	Clear()
}

func (s *Service) CreateApplicationCredential(
	token, userID string, request domain.ApplicationCredentialRequest,
) (domain.ApplicationCredential, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, err := s.session(token)
	if err != nil {
		return domain.ApplicationCredential{}, "", err
	}
	if userID != session.userID || session.restricted() {
		return domain.ApplicationCredential{}, "", ErrForbidden
	}
	roles, err := s.credentialRoles(request.Roles)
	if err != nil {
		return domain.ApplicationCredential{}, "", err
	}
	if request.ExpiresAt != nil && request.ExpiresAt.Before(s.now()) {
		return domain.ApplicationCredential{}, "", ErrInvalidApplicationCredential
	}

	secret := request.Secret
	if secret == "" {
		generated := make([]byte, secretBytes)
		if _, err := io.ReadFull(s.entropy, generated); err != nil {
			return domain.ApplicationCredential{}, "", ErrApplicationCredentialGeneration
		}
		secret = base64.RawURLEncoding.EncodeToString(generated)
	}
	id := make([]byte, 16)
	salt := make([]byte, secretSaltBytes)
	if _, err := io.ReadFull(s.entropy, id); err != nil {
		return domain.ApplicationCredential{}, "", ErrApplicationCredentialGeneration
	}
	if _, err := io.ReadFull(s.entropy, salt); err != nil {
		return domain.ApplicationCredential{}, "", ErrApplicationCredentialGeneration
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	hash, err := hashSecret(secret, salt)
	if err != nil {
		return domain.ApplicationCredential{}, "", ErrApplicationCredentialGeneration
	}

	credential := domain.ApplicationCredential{
		ID:           hex.EncodeToString(id),
		Name:         request.Name,
		Description:  request.Description,
		UserID:       session.userID,
		ProjectID:    session.projectID,
		Roles:        roles,
		ExpiresAt:    request.ExpiresAt,
		Unrestricted: request.Unrestricted,
		SecretSalt:   salt,
		SecretHash:   hash,
	}
	if !s.credentials.Add(credential) {
		return domain.ApplicationCredential{}, "", ErrApplicationCredentialExists
	}
	return credential, secret, nil
}

func (s *Service) ListApplicationCredentials(token, userID, name string) ([]domain.ApplicationCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, err := s.session(token)
	if err != nil {
		return nil, err
	}
	if userID != session.userID {
		return nil, ErrForbidden
	}
	return s.credentials.List(userID, name), nil
}

func (s *Service) GetApplicationCredential(token, userID, id string) (domain.ApplicationCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, credential, err := s.ownedCredential(token, userID, id)
	return credential, err
}

func (s *Service) DeleteApplicationCredential(token, userID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, credential, err := s.ownedCredential(token, userID, id)
	if err != nil && !errors.Is(err, ErrApplicationCredentialNotFound) {
		return err
	}
	if session.restricted() {
		return ErrForbidden
	}
	if err != nil {
		return err
	}
	s.credentials.Remove(credential.ID)
	return nil
}

func (s *Service) ownedCredential(token, userID, id string) (session, domain.ApplicationCredential, error) {
	current, err := s.session(token)
	if err != nil {
		return session{}, domain.ApplicationCredential{}, err
	}
	credential, exists := s.credentials.Get(id)
	if exists {
		userID = credential.UserID
	}
	if userID != current.userID {
		return session{}, domain.ApplicationCredential{}, ErrForbidden
	}
	if !exists {
		return current, domain.ApplicationCredential{}, ErrApplicationCredentialNotFound
	}
	return current, credential, nil
}

func (s *Service) credentialRoles(selectors []domain.Selector) ([]domain.Role, error) {
	role := s.fixture.Role
	for _, selector := range selectors {
		switch {
		case selector.ID != "" && selector.ID != role.ID:
			return nil, ErrInvalidApplicationCredential
		case selector.ID == "" && selector.Name != role.Name:
			return nil, ErrRoleNotFound
		}
	}
	return []domain.Role{role}, nil
}

func verifySecret(credential domain.ApplicationCredential, secret string) bool {
	hash, err := hashSecret(secret, credential.SecretSalt)
	return err == nil && subtle.ConstantTimeCompare(hash, credential.SecretHash) == 1
}

func hashSecret(secret string, salt []byte) ([]byte, error) {
	return pbkdf2.Key(sha512.New, secret, slices.Clone(salt), secretHashRounds, secretHashBytes)
}
