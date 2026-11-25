package server

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	appvolume "github.com/JSYoo5B/sandstack/internal/app/volume"
)

func registerVolumeExtensionControls(router chi.Router, service *appvolume.Service) {
	const volumePath = "/volume/volumes/{volumeID}"
	finish := func(failed bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			err := service.FinishExtension(chi.URLParam(r, "volumeID"), failed)
			switch {
			case errors.Is(err, appvolume.ErrVolumeNotFound):
				http.Error(w, "Volume not found", http.StatusNotFound)
			case errors.Is(err, appvolume.ErrVolumeStateConflict):
				http.Error(w, "The volume has no pending extension", http.StatusConflict)
			case err != nil:
				http.Error(w, "Unable to set the volume extension outcome", http.StatusInternalServerError)
			default:
				noContent(w, r)
			}
		}
	}

	router.Put(volumePath+"/complete-extend", finish(false))
	router.Put(volumePath+"/fail-extend", finish(true))
}
