package compute

import "time"

type Status string

const (
	StatusActive  Status = "active"
	StatusError   Status = "error"
	StatusShutoff Status = "shutoff"
	StatusPaused  Status = "paused"
)

type Server struct {
	ID        string
	Name      string
	ProjectID string
	UserID    string
	ImageID   string
	CreatedAt time.Time
	UpdatedAt time.Time
	State     Status
	Fault     *Fault
	Metadata  map[string]string
}

func (s Server) Status() Status {
	if s.Fault != nil {
		return StatusError
	}
	if s.State == "" {
		return StatusActive
	}
	return s.State
}

type Fault struct {
	Code      int
	Message   string
	CreatedAt time.Time
}
