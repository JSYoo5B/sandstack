package volume

import "time"

type State string

const (
	StateAvailable      State = "available"
	StateCreating       State = "creating"
	StateError          State = "error"
	StateInUse          State = "in-use"
	StateExtending      State = "extending"
	StateErrorExtending State = "error_extending"
)

type Volume struct {
	ID          string
	Name        NullableText
	Description NullableText
	ProjectID   string
	UserID      string
	SnapshotID  string
	Size        int
	CreatedAt   time.Time
	UpdatedAt   time.Time
	State       State
	Attachment  Attachment
	Metadata    map[string]string
}

// NullableText distinguishes a stored empty string from an unset field.
type NullableText struct {
	Value string
	Valid bool
}
