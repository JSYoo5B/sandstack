package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/JSYoo5B/sandstack/internal/api/server"
)

func TestAdditionalFixturesReadsNumberedVariables(t *testing.T) {
	environ := []string{
		"SANDSTACK_USERNAME=primary-user",
		"SANDSTACK_FIXTURE_2_PROJECT=second-project",
		"SANDSTACK_FIXTURE_1_USERNAME=first-user",
		"SANDSTACK_FIXTURE_1_PASSWORD=first=password",
		"SANDSTACK_FIXTURE_1_PROJECT=first-project",
		"SANDSTACK_FIXTURE_2_USERNAME=second-user",
		"SANDSTACK_FIXTURE_2_PASSWORD=second-password",
	}

	fixtures, err := additionalFixtures(environ)

	require.NoError(t, err)
	assert.Equal(t, []server.Fixture{
		{Username: "first-user", Password: "first=password", ProjectName: "first-project"},
		{Username: "second-user", Password: "second-password", ProjectName: "second-project"},
	}, fixtures)
}

func TestAdditionalFixturesKeepsGapsForValidation(t *testing.T) {
	fixtures, err := additionalFixtures([]string{"SANDSTACK_FIXTURE_2_USERNAME=second-user"})

	require.NoError(t, err)
	assert.Equal(t, []server.Fixture{{}, {Username: "second-user"}}, fixtures)
}

func TestAdditionalFixturesRejectsUnsupportedVariables(t *testing.T) {
	for _, key := range []string{
		"SANDSTACK_FIXTURE_0_USERNAME",
		"SANDSTACK_FIXTURE_01_USERNAME",
		"SANDSTACK_FIXTURE_X_USERNAME",
		"SANDSTACK_FIXTURE_1_ROLE",
		"SANDSTACK_FIXTURE_1",
	} {
		_, err := additionalFixtures([]string{key + "=value"})

		assert.ErrorContains(t, err, key)
	}
}

func TestAdditionalFixturesWithoutVariables(t *testing.T) {
	fixtures, err := additionalFixtures([]string{"SANDSTACK_PROJECT=primary-project"})

	require.NoError(t, err)
	assert.Empty(t, fixtures)
}
