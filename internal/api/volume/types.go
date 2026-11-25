package volume

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

type volumeTypeBody struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	IsPublic     bool              `json:"is_public"`
	LegacyPublic *bool             `json:"os-volume-type-access:is_public,omitempty"`
	ExtraSpecs   map[string]string `json:"extra_specs"`
}

func (h handler) showVolumeType(w http.ResponseWriter, r *http.Request, projectID string) {
	typeID := chi.URLParam(r, "typeID")
	volumeType, err := h.service.GetVolumeType(projectID, typeID)
	switch {
	case errors.Is(err, app.ErrVolumeTypeNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The volume type could not be found.")
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to read the volume type.")
	default:
		writeJSON(w, http.StatusOK, struct {
			VolumeType volumeTypeBody `json:"volume_type"`
		}{VolumeType: describeVolumeType(volumeType, typeID == volumeType.ID)})
	}
}

func describeVolumeType(volumeType domain.VolumeType, legacyPublic bool) volumeTypeBody {
	body := volumeTypeBody{
		ID:          volumeType.ID,
		Name:        volumeType.Name,
		Description: volumeType.Description,
		IsPublic:    volumeType.Public,
		ExtraSpecs:  map[string]string{},
	}
	if legacyPublic {
		body.LegacyPublic = &body.IsPublic
	}
	return body
}
