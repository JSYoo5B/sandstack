package volume

import "time"

type State string

const (
	StateAvailable        State = "available"
	StateCreating         State = "creating"
	StateError            State = "error"
	StateInUse            State = "in-use"
	StateReserved         State = "reserved"
	StateExtending        State = "extending"
	StateErrorExtending   State = "error_extending"
	StateBackingUp        State = "backing-up"
	StateRestoring        State = "restoring"
	StateRestoringBackup  State = "restoring-backup"
	StateErrorRestoring   State = "error_restoring"
	StateAwaitingTransfer State = "awaiting-transfer"
)

type Volume struct {
	ID                  string
	Name                NullableText
	Description         NullableText
	ProjectID           string
	UserID              string
	SnapshotID          string
	SourceVolumeID      string
	Size                int
	TypeID              string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	State               State
	PreviousState       State
	Attachment          Attachment
	Metadata            map[string]string
	ImageSource         ImageSource
	ImageMetadataReady  bool
	Bootable            bool
	BootableOnProvision bool
}

// ImageSource is the Glance image metadata captured for volume provisioning.
type ImageSource struct {
	ID              string
	Name            string
	Checksum        string
	ContainerFormat string
	DiskFormat      string
	Size            int64
	MinDiskGiB      int
	MinRAMMiB       int
}

// NullableText distinguishes a stored empty string from an unset field.
type NullableText struct {
	Value string
	Valid bool
}
