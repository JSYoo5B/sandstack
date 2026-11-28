package volume

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (h handler) volumeAction(w http.ResponseWriter, r *http.Request, projectID string) {
	data, ok := readJSONBody(w, r, "volume action")
	if !ok {
		return
	}
	actions, err := readUniqueObject(data)
	if err != nil || len(actions) != 1 {
		writeFault(w, http.StatusBadRequest, "badRequest", "Expected a single volume action.")
		return
	}
	volumeID := chi.URLParam(r, "volumeID")
	switch {
	case actions["os-extend"] != nil:
		h.extendVolume(w, projectID, volumeID, actions["os-extend"])
	case actions["os-set_bootable"] != nil:
		h.setVolumeBootable(w, projectID, volumeID, actions["os-set_bootable"])
	case actions["os-retype"] != nil:
		h.retypeVolume(w, projectID, volumeID, actions["os-retype"])
	case actions["os-volume_upload_image"] != nil:
		h.uploadVolumeImage(w, projectID, volumeID, actions["os-volume_upload_image"])
	case actions["os-show_image_metadata"] != nil:
		h.showVolumeImageMetadata(w, projectID, volumeID)
	default:
		writeFault(w, http.StatusBadRequest, "badRequest", "The volume action is not supported.")
	}
}
