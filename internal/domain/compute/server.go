package compute

import "time"

type Server struct {
	ID        string
	Name      string
	ProjectID string
	UserID    string
	CreatedAt time.Time
	UpdatedAt time.Time
	Fault     *Fault
}

type Fault struct {
	Code      int
	Message   string
	CreatedAt time.Time
}
