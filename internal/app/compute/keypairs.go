package compute

import (
	"cmp"
	"errors"
	"io"
	"maps"
	"slices"
	"sync"
	"time"

	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
	"github.com/JSYoo5B/sandstack/internal/platform/sshkey"
)

const maxKeyPairs = 100

var (
	ErrKeyPairNotFound   = errors.New("key pair not found")
	ErrKeyPairExists     = errors.New("key pair already exists")
	ErrKeyPairQuota      = errors.New("quota exceeded, too many key pairs")
	ErrInvalidPublicKey  = errors.New("key pair data is invalid: failed to generate fingerprint")
	ErrKeyPairGeneration = errors.New("key pair generation failed")
)

type KeyPairs struct {
	mu     sync.Mutex
	now    func() time.Time
	random io.Reader
	pairs  map[string]map[string]domain.KeyPair
	lastID int
}

func NewKeyPairs(now func() time.Time, random io.Reader) *KeyPairs {
	return &KeyPairs{now: now, random: random, pairs: make(map[string]map[string]domain.KeyPair)}
}

func (k *KeyPairs) Import(userID, name, publicKey string) (domain.KeyPair, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	if len(k.pairs[userID]) >= maxKeyPairs {
		return domain.KeyPair{}, ErrKeyPairQuota
	}
	fingerprint, err := sshkey.Fingerprint(publicKey)
	if err != nil {
		return domain.KeyPair{}, ErrInvalidPublicKey
	}
	return k.save(userID, name, publicKey, fingerprint)
}

func (k *KeyPairs) Generate(userID, name string) (domain.KeyPair, string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	if len(k.pairs[userID]) >= maxKeyPairs {
		return domain.KeyPair{}, "", ErrKeyPairQuota
	}
	privateKey, publicKey, err := sshkey.Generate(k.random)
	if err != nil {
		return domain.KeyPair{}, "", errors.Join(ErrKeyPairGeneration, err)
	}
	fingerprint, err := sshkey.Fingerprint(publicKey)
	if err != nil {
		return domain.KeyPair{}, "", errors.Join(ErrKeyPairGeneration, err)
	}
	pair, err := k.save(userID, name, publicKey, fingerprint)
	if err != nil {
		return domain.KeyPair{}, "", err
	}
	return pair, privateKey, nil
}

func (k *KeyPairs) save(userID, name, publicKey, fingerprint string) (domain.KeyPair, error) {
	if _, found := k.pairs[userID][name]; found {
		return domain.KeyPair{}, ErrKeyPairExists
	}
	k.lastID++
	pair := domain.KeyPair{
		ID:          k.lastID,
		Name:        name,
		UserID:      userID,
		PublicKey:   publicKey,
		Fingerprint: fingerprint,
		CreatedAt:   k.now().UTC(),
	}
	if k.pairs[userID] == nil {
		k.pairs[userID] = make(map[string]domain.KeyPair)
	}
	k.pairs[userID][name] = pair
	return pair, nil
}

func (k *KeyPairs) List(userID string) []domain.KeyPair {
	k.mu.Lock()
	defer k.mu.Unlock()

	return slices.SortedFunc(maps.Values(k.pairs[userID]), func(a, b domain.KeyPair) int {
		return cmp.Compare(a.Name, b.Name)
	})
}

func (k *KeyPairs) Get(userID, name string) (domain.KeyPair, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	pair, found := k.pairs[userID][name]
	if !found {
		return domain.KeyPair{}, ErrKeyPairNotFound
	}
	return pair, nil
}

func (k *KeyPairs) Delete(userID, name string) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	if _, found := k.pairs[userID][name]; !found {
		return ErrKeyPairNotFound
	}
	delete(k.pairs[userID], name)
	return nil
}

func (k *KeyPairs) Reset() {
	k.mu.Lock()
	defer k.mu.Unlock()

	k.pairs = make(map[string]map[string]domain.KeyPair)
	k.lastID = 0
}
