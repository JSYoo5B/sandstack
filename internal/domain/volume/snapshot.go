package volume

import "time"

type Snapshot struct {
	ID          string
	ProjectID   string
	VolumeID    string
	Name        string
	Description string
	Size        int
	CreatedAt   time.Time
	UpdatedAt   time.Time
	State       State
}
