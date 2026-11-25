package image

import "time"

type Image struct {
	ID          string
	Name        string
	ProjectID   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Deactivated bool
}
