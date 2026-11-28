package image

import "time"

const (
	MemberPending  = "pending"
	MemberAccepted = "accepted"
	MemberRejected = "rejected"
)

type Member struct {
	ImageID   string
	MemberID  string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}
