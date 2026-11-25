package volume

import "time"

type State string

const (
	StateAvailable State = "available"
	StateCreating  State = "creating"
	StateError     State = "error"
	StateInUse     State = "in-use"
)

type Volume struct {
	ID          string
	Name        string
	Description string
	ProjectID   string
	UserID      string
	Size        int
	CreatedAt   time.Time
	UpdatedAt   time.Time
	State       State
	Attachment  Attachment
}
