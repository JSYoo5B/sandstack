package compute

import "time"

type Status string

const (
	StatusActive  Status = "active"
	StatusError   Status = "error"
	StatusShutoff Status = "shutoff"
)

type Server struct {
	ID        string
	Name      string
	ProjectID string
	UserID    string
	ImageID   string
	CreatedAt time.Time
	UpdatedAt time.Time
	Stopped   bool
	Fault     *Fault
}

func (s Server) Status() Status {
	if s.Fault != nil {
		return StatusError
	}
	if s.Stopped {
		return StatusShutoff
	}
	return StatusActive
}

type Fault struct {
	Code      int
	Message   string
	CreatedAt time.Time
}
