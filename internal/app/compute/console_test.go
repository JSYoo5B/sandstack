package compute_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/compute"
	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L7561-L7584
func TestConsoleOutputTailsLinesLikeNova(t *testing.T) {
	seed := domain.Server{ID: "server", ProjectID: "project"}
	service := app.New(seed, time.Now, nil)
	require.NoError(t, service.SetConsoleOutput(seed.ID, []byte("0\n1\n2\n3\n4\n")))
	length := func(n int64) *int64 { return &n }

	for _, test := range []struct {
		name   string
		length *int64
		want   string
	}{
		{name: "unset", want: "0\n1\n2\n3\n4\n"},
		{name: "zero", length: length(0), want: ""},
		{name: "trailing segment counts", length: length(1), want: ""},
		{name: "last lines", length: length(3), want: "3\n4\n"},
		{name: "longer than log", length: length(100), want: "0\n1\n2\n3\n4\n"},
		{name: "minus one skips first line", length: length(-1), want: "1\n2\n3\n4\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			output, err := service.ConsoleOutput(seed.ProjectID, seed.ID, test.length)

			require.NoError(t, err)
			assert.Equal(t, test.want, output)
		})
	}
}

func TestConsoleOutputControlAndReset(t *testing.T) {
	seed := domain.Server{ID: "server", ProjectID: "project", State: domain.StatusShutoff, Locked: true}
	service := app.New(seed, time.Now, nil)
	read := func() string {
		t.Helper()
		output, err := service.ConsoleOutput(seed.ProjectID, seed.ID, nil)
		require.NoError(t, err)
		return output
	}
	initial := read()

	setErr := service.SetConsoleOutput(seed.ID, []byte("caf\xc3\xa9\r\n"))
	replaced := read()
	missingErr := service.SetConsoleOutput("other", []byte("ignored"))
	_, foreignErr := service.ConsoleOutput("foreign", seed.ID, nil)
	service.Reset()
	afterReset := read()
	require.NoError(t, service.SetConsoleOutput(seed.ID, []byte("changed")))
	clearErr := service.SetConsoleOutput(seed.ID, nil)

	assert.Equal(t, app.FixtureConsoleOutput, initial)
	require.NoError(t, setErr)
	assert.Equal(t, "caf��\r\n", replaced)
	assert.ErrorIs(t, missingErr, app.ErrServerNotFound)
	assert.ErrorIs(t, foreignErr, app.ErrServerNotFound)
	assert.Equal(t, app.FixtureConsoleOutput, afterReset)
	require.NoError(t, clearErr)
	assert.Equal(t, app.FixtureConsoleOutput, read())
}
