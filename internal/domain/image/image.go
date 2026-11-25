package image

import "time"

type Image struct {
	ID          string
	Name        string
	Tags        []string
	ProjectID   string
	Size        int64
	Checksum    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Deactivated bool
}

func (i Image) Status() string {
	if i.Deactivated {
		return "deactivated"
	}
	return "active"
}
