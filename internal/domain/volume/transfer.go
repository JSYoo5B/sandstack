package volume

import "time"

type Transfer struct {
	ID        string
	ProjectID string
	VolumeID  string
	Name      NullableText
	AuthKey   string
	CreatedAt time.Time
}
