package image

import "time"

type Image struct {
	ID          string
	Name        string
	ProjectID   string
	Size        int64
	Checksum    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Deactivated bool
}
