package identity

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenRequestBodyBoundaries(t *testing.T) {
	const valid = `{
		"auth": {
			"identity": {
				"methods": ["password"],
				"password": {"user": {"id": "fixture-user", "password": "fixture-password"}}
			},
			"scope": {"project": {"id": "fixture-project"}}
		}
	}`
	limit := valid + strings.Repeat(" ", maxTokenRequestBytes-len(valid))

	t.Run("at size limit", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(limit))

		grant, err := readTokenRequest(httptest.NewRecorder(), r)

		require.NoError(t, err)
		require.NotNil(t, grant.password)
		assert.Equal(t, "fixture-user", grant.password.User.ID)
		assert.Equal(t, "fixture-project", grant.password.Project.ID)
	})

	t.Run("padding exceeds size limit", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(limit+" "))

		_, err := readTokenRequest(httptest.NewRecorder(), r)

		var tooLarge *http.MaxBytesError
		require.ErrorAs(t, err, &tooLarge)
		assert.EqualValues(t, maxTokenRequestBytes, tooLarge.Limit)
	})

	t.Run("second JSON value", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(valid+`{}`))

		_, err := readTokenRequest(httptest.NewRecorder(), r)

		assert.ErrorIs(t, err, errInvalidTokenRequest)
	})
}
