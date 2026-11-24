package compute

import "time"

type Status string

const (
	StatusActive Status = "active"
	StatusError  Status = "error"
)

type Server struct {
	ID        string
	Name      string
	ProjectID string
	UserID    string
	CreatedAt time.Time
	UpdatedAt time.Time
	Fault     *Fault
}

func (s Server) Status() Status {
	if s.Fault != nil {
		return StatusError
	}
	return StatusActive
}

type Fault struct {
	Code      int
	Message   string
	CreatedAt time.Time
}
