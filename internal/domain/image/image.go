package image

import "time"

const (
	ContainerFormatBare = "bare"
	DiskFormatQCOW2     = "qcow2"
	DiskFormatRaw       = "raw"
)

type Image struct {
	ID              string
	Name            string
	Tags            []string
	ProjectID       string
	Size            int64
	Checksum        string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Deactivated     bool
	Queued          bool
	Protected       bool
	ContainerFormat string
	DiskFormat      string
	MinDiskGiB      int
	MinRAMMiB       int
	Properties      map[string]string
}

func (i Image) Status() string {
	if i.Queued {
		return "queued"
	}
	if i.Deactivated {
		return "deactivated"
	}
	return "active"
}
