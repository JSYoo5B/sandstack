package compute_test

import (
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/compute"
	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

const importedKey = "ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAy" +
	"NTYAAABBBG1r4wzPTIjSo78POCq+u/czb8gYK0KvqlmCvcRPrnDWxgLw7y6BX51t" +
	"uYREz7iLRCP7BwUt8R+ZWzFZDeOLIWU= imported"

func TestKeyPairsAreScopedToTheUser(t *testing.T) {
	created := time.Date(2026, time.November, 25, 1, 2, 3, 456789123, time.FixedZone("fixture", 3600))
	service := app.NewKeyPairs(func() time.Time { return created }, rand.NewChaCha8([32]byte{1}))

	imported, importErr := service.Import("user", "b-imported", importedKey)
	generated, privateKey, generateErr := service.Generate("user", "a-generated")
	other, otherErr := service.Import("other", "b-imported", importedKey)

	require.NoError(t, importErr)
	require.NoError(t, generateErr)
	require.NoError(t, otherErr)
	assert.Equal(t, domain.KeyPair{
		ID: 1, Name: "b-imported", UserID: "user", PublicKey: importedKey,
		Fingerprint: "16:6a:c9:ec:80:4d:17:3e:d5:3b:6f:c0:d7:15:04:40", CreatedAt: created.UTC(),
	}, imported)
	assert.Equal(t, 2, generated.ID)
	assert.True(t, strings.HasPrefix(generated.PublicKey, "ssh-rsa "))
	assert.True(t, strings.HasPrefix(privateKey, "-----BEGIN RSA PRIVATE KEY-----\n"))
	assert.Regexp(t, `^([0-9a-f]{2}:){15}[0-9a-f]{2}$`, generated.Fingerprint)
	assert.Equal(t, 3, other.ID)
	assert.Equal(t, []domain.KeyPair{generated, imported}, service.List("user"))
	assert.Equal(t, []domain.KeyPair{other}, service.List("other"))
	assert.Empty(t, service.List("nobody"))
	read, err := service.Get("user", "a-generated")
	require.NoError(t, err)
	assert.Equal(t, generated, read)
	_, err = service.Get("nobody", "a-generated")
	assert.ErrorIs(t, err, app.ErrKeyPairNotFound)
}

func TestKeyPairCreationChecksQuotaKeyAndName(t *testing.T) {
	service := app.NewKeyPairs(time.Now, rand.NewChaCha8([32]byte{2}))
	require.NoError(t, importKeys(service, "user", 0, 1))

	_, duplicateImportErr := service.Import("user", "key-0", importedKey)
	_, _, duplicateGenerateErr := service.Generate("user", "key-0")
	_, invalidErr := service.Import("user", "key-0", "ssh-rsa invalid")
	_, emptyErr := service.Import("user", "empty", "")

	assert.ErrorIs(t, duplicateImportErr, app.ErrKeyPairExists)
	assert.ErrorIs(t, duplicateGenerateErr, app.ErrKeyPairExists)
	assert.ErrorIs(t, invalidErr, app.ErrInvalidPublicKey)
	assert.ErrorIs(t, emptyErr, app.ErrInvalidPublicKey)
	assert.Len(t, service.List("user"), 1)

	require.NoError(t, importKeys(service, "user", 1, 100))
	_, quotaImportErr := service.Import("user", "key-0", "ssh-rsa invalid")
	_, _, quotaGenerateErr := service.Generate("user", "extra")
	_, otherUserErr := service.Import("other", "extra", importedKey)

	assert.ErrorIs(t, quotaImportErr, app.ErrKeyPairQuota)
	assert.ErrorIs(t, quotaGenerateErr, app.ErrKeyPairQuota)
	assert.NoError(t, otherUserErr)
	assert.Len(t, service.List("user"), 100)
}

func TestKeyPairDeleteAndResetClearState(t *testing.T) {
	service := app.NewKeyPairs(time.Now, rand.NewChaCha8([32]byte{3}))
	require.NoError(t, importKeys(service, "user", 0, 2))

	deleteErr := service.Delete("user", "key-0")
	missingErr := service.Delete("user", "key-0")
	foreignErr := service.Delete("other", "key-1")

	assert.NoError(t, deleteErr)
	assert.ErrorIs(t, missingErr, app.ErrKeyPairNotFound)
	assert.ErrorIs(t, foreignErr, app.ErrKeyPairNotFound)
	_, err := service.Get("user", "key-0")
	assert.ErrorIs(t, err, app.ErrKeyPairNotFound)
	remaining := service.List("user")
	require.Len(t, remaining, 1)
	assert.Equal(t, 2, remaining[0].ID)

	service.Reset()

	assert.Empty(t, service.List("user"))
	restarted, err := service.Import("user", "key-1", importedKey)
	require.NoError(t, err)
	assert.Equal(t, 1, restarted.ID)
}

func TestKeyPairGenerationFollowsTheInjectedRandomStream(t *testing.T) {
	first := app.NewKeyPairs(time.Now, rand.NewChaCha8([32]byte{4}))
	second := app.NewKeyPairs(time.Now, rand.NewChaCha8([32]byte{4}))

	firstPair, firstPrivate, firstErr := first.Generate("user", "key")
	secondPair, secondPrivate, secondErr := second.Generate("user", "key")

	require.NoError(t, firstErr)
	require.NoError(t, secondErr)
	assert.Equal(t, firstPrivate, secondPrivate)
	assert.Equal(t, firstPair.PublicKey, secondPair.PublicKey)
	assert.Equal(t, firstPair.Fingerprint, secondPair.Fingerprint)
}

func importKeys(service *app.KeyPairs, userID string, from, to int) error {
	for i := from; i < to; i++ {
		if _, err := service.Import(userID, "key-"+strconv.Itoa(i), importedKey); err != nil {
			return err
		}
	}
	return nil
}
