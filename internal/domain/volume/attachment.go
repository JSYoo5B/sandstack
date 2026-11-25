package volume

import "time"

type Attachment struct {
	ID         string
	VolumeID   string
	ServerID   string
	Device     string
	AttachedAt time.Time
}
