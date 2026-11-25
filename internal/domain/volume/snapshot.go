package volume

import "time"

type Snapshot struct {
	ID          string
	ProjectID   string
	VolumeID    string
	Name        NullableText
	Description NullableText
	Size        int
	CreatedAt   time.Time
	UpdatedAt   time.Time
	State       State
	Metadata    map[string]string
	ImageSource ImageSource
}
