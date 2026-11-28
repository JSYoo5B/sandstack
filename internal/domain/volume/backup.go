package volume

import "time"

type Backup struct {
	ID               string
	ProjectID        string
	VolumeID         string
	SnapshotID       string
	ParentID         string
	Name             NullableText
	Description      NullableText
	Container        NullableText
	Size             int
	State            State
	FailReason       string
	DependentBackups int
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DataTimestamp    time.Time
	RestoreVolumeID  string
	Source           BackupSource
}

// BackupSource is the volume metadata a completed backup can restore.
type BackupSource struct {
	Name               NullableText
	Description        NullableText
	Metadata           map[string]string
	ImageSource        ImageSource
	ImageMetadataReady bool
}

type Restore struct {
	BackupID   string
	VolumeID   string
	VolumeName NullableText
}
