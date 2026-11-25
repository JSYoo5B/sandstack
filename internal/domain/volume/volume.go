package volume

import "time"

type Volume struct {
	ID        string
	Name      string
	ProjectID string
	UserID    string
	Size      int
	CreatedAt time.Time
	UpdatedAt time.Time
	Failed    bool
}
