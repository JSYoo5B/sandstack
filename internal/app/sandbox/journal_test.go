package sandbox_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/JSYoo5B/sandstack/internal/app/sandbox"
)

func TestJournalWrapsInRecordingOrderAndReturnsDetachedRecords(t *testing.T) {
	journal := sandbox.NewRequestJournal(3)
	assert.Equal(t, []sandbox.RequestRecord{}, journal.Records())

	journal.Record("GET", "/service/items", 200)
	assert.Equal(t, []sandbox.RequestRecord{
		{Sequence: 1, Method: "GET", Route: "/service/items", Status: 200},
	}, journal.Records())
	journal.Record("GET", "/service/items/{id}", 200)
	journal.Record("GET", "/service/items/{id}", 404)
	journal.Record("POST", "/service/items", 201)
	journal.Record("GET", "/service/items", 200)

	expected := []sandbox.RequestRecord{
		{Sequence: 3, Method: "GET", Route: "/service/items/{id}", Status: 404},
		{Sequence: 4, Method: "POST", Route: "/service/items", Status: 201},
		{Sequence: 5, Method: "GET", Route: "/service/items", Status: 200},
	}
	snapshot := journal.Records()
	require.Len(t, snapshot, len(expected))
	assert.Equal(t, expected, snapshot)

	snapshot[0].Route = "/changed"
	assert.Equal(t, expected, journal.Records())
}

func TestJournalResetClearsRecordsAndSequence(t *testing.T) {
	journal := sandbox.NewRequestJournal(1)
	journal.Record("GET", "/service/items", 200)
	journal.Record("GET", "/service/items/{id}", 404)
	journal.Reset()
	journal.Reset()

	assert.Equal(t, []sandbox.RequestRecord{}, journal.Records())
	journal.Record("POST", "/service/items", 201)
	assert.Equal(t, []sandbox.RequestRecord{
		{Sequence: 1, Method: "POST", Route: "/service/items", Status: 201},
	}, journal.Records())
}

func TestJournalWithNonpositiveCapacityStaysEmpty(t *testing.T) {
	journal := sandbox.NewRequestJournal(0)
	journal.Record("GET", "/service/items", 200)
	journal.Reset()

	assert.Equal(t, []sandbox.RequestRecord{}, journal.Records())
}

func TestJournalRecordsConcurrentCompletions(t *testing.T) {
	const capacity, attempts = 4, 20
	journal := sandbox.NewRequestJournal(capacity)

	var wait sync.WaitGroup
	for range attempts {
		wait.Go(func() {
			journal.Record("GET", "/service/items", 200)
			assert.LessOrEqual(t, len(journal.Records()), capacity)
		})
	}
	wait.Wait()

	records := journal.Records()
	require.Len(t, records, capacity)
	for i, record := range records {
		assert.Equal(t, uint64(attempts-capacity+i+1), record.Sequence)
	}
}
