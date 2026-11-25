package sandbox

import "sync"

type RequestRecord struct {
	Sequence uint64
	Method   string
	Route    string
	Status   int
}

// RequestJournal retains the most recent completed operations in recording order.
type RequestJournal struct {
	mu       sync.RWMutex
	entries  []RequestRecord
	next     int
	count    int
	sequence uint64
}

// NewRequestJournal makes a bounded journal. A nonpositive capacity disables it.
func NewRequestJournal(capacity int) *RequestJournal {
	return &RequestJournal{entries: make([]RequestRecord, max(0, capacity))}
}

// Record takes a normalized method and a registered route template.
func (j *RequestJournal) Record(method, route string, status int) {
	if len(j.entries) == 0 {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()

	j.sequence++
	j.entries[j.next] = RequestRecord{
		Sequence: j.sequence,
		Method:   method,
		Route:    route,
		Status:   status,
	}
	j.next = (j.next + 1) % len(j.entries)
	j.count = min(j.count+1, len(j.entries))
}

// Records takes a detached snapshot from oldest to newest retained record.
func (j *RequestJournal) Records() []RequestRecord {
	j.mu.RLock()
	defer j.mu.RUnlock()

	result := make([]RequestRecord, j.count)
	first := j.next - j.count
	if first < 0 {
		first += len(j.entries)
	}
	copied := copy(result, j.entries[first:])
	copy(result[copied:], j.entries[:j.next])
	return result
}

// Reset discards all records and restarts sequencing at one.
func (j *RequestJournal) Reset() {
	j.mu.Lock()
	defer j.mu.Unlock()

	clear(j.entries)
	j.next = 0
	j.count = 0
	j.sequence = 0
}
