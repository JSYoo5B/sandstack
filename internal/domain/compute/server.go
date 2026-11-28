package compute

import "time"

type Status string

const (
	StatusBuild     Status = "build"
	StatusActive    Status = "active"
	StatusError     Status = "error"
	StatusShutoff   Status = "shutoff"
	StatusPaused    Status = "paused"
	StatusSuspended Status = "suspended"
)

const TaskImageSnapshotPending = "image_snapshot_pending"

type Server struct {
	ID             string
	Name           string
	ProjectID      string
	UserID         string
	ImageID        string
	KeyName        string
	AutoDiskConfig bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
	State          Status
	Locked         bool
	Fault          *Fault
	Metadata       map[string]string
	Ports          []ServerPort
	BootVolume     *BootVolume
	ImageMeta      *ImageMeta
	TaskState      string
	PendingImageID string
}

type ImageMeta struct {
	MinRAMMiB       int
	MinDiskGiB      int
	ContainerFormat string
	DiskFormat      string
	Properties      map[string]string
}

type BootVolume struct {
	VolumeID            string
	ImageID             string
	Size                int
	DeleteOnTermination bool
}

// ServerPort is a Neutron port bound to the server. Nova unbinds preserved
// ports on deletion and deletes the ports it created.
type ServerPort struct {
	ID       string
	Preserve bool
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
