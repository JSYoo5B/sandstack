package volume

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

const timestampLayout = "2006-01-02T15:04:05.000000"

type volumeBody struct {
	ID                 string            `json:"id"`
	Name               string            `json:"name"`
	ProjectID          string            `json:"os-vol-tenant-attr:tenant_id"`
	UserID             string            `json:"user_id"`
	Size               int               `json:"size"`
	Status             string            `json:"status"`
	CreatedAt          string            `json:"created_at"`
	UpdatedAt          string            `json:"updated_at"`
	AvailabilityZone   string            `json:"availability_zone"`
	Description        *string           `json:"description"`
	VolumeType         string            `json:"volume_type"`
	SnapshotID         *string           `json:"snapshot_id"`
	SourceVolumeID     *string           `json:"source_volid"`
	ConsistencyGroupID *string           `json:"consistencygroup_id"`
	Attachments        []struct{}        `json:"attachments"`
	Metadata           map[string]string `json:"metadata"`
	Links              []volumeLink      `json:"links"`
	Bootable           string            `json:"bootable"`
	Encrypted          bool              `json:"encrypted"`
	ReplicationStatus  string            `json:"replication_status"`
	Multiattach        bool              `json:"multiattach"`
}

type volumeLink struct {
	Href string `json:"href"`
	Rel  string `json:"rel"`
}

func (h handler) describeVolume(r *http.Request, volume domain.Volume) volumeBody {
	status := "available"
	if volume.Failed {
		status = "error"
	}
	resourcePath := "/" + volume.ProjectID + "/volumes/" + volume.ID
	return volumeBody{
		ID:                volume.ID,
		Name:              volume.Name,
		ProjectID:         volume.ProjectID,
		UserID:            volume.UserID,
		Size:              volume.Size,
		Status:            status,
		CreatedAt:         volume.CreatedAt.UTC().Format(timestampLayout),
		UpdatedAt:         volume.UpdatedAt.UTC().Format(timestampLayout),
		AvailabilityZone:  "nova",
		VolumeType:        "standard",
		Attachments:       []struct{}{},
		Metadata:          map[string]string{},
		Bootable:          "false",
		ReplicationStatus: "disabled",
		Links: []volumeLink{
			{Href: h.endpointURL(r, Path+resourcePath), Rel: "self"},
			{Href: h.endpointURL(r, serviceRoot+resourcePath), Rel: "bookmark"},
		},
	}
}

func (h handler) listVolumes(w http.ResponseWriter, r *http.Request, projectID string) {
	volumes := h.service.List(projectID)
	response := struct {
		Volumes []volumeBody `json:"volumes"`
	}{Volumes: make([]volumeBody, len(volumes))}
	for i, volume := range volumes {
		response.Volumes[i] = h.describeVolume(r, volume)
	}
	writeJSON(w, http.StatusOK, response)
}

func (h handler) showVolume(w http.ResponseWriter, r *http.Request, projectID string) {
	volume, err := h.service.Get(projectID, chi.URLParam(r, "volumeID"))
	if errors.Is(err, app.ErrVolumeNotFound) {
		writeFault(w, http.StatusNotFound, "itemNotFound", "The volume could not be found.")
		return
	}
	if err != nil {
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to read the volume.")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Volume volumeBody `json:"volume"`
	}{Volume: h.describeVolume(r, volume)})
}
