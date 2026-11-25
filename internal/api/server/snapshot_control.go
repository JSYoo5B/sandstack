package server

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	appvolume "github.com/JSYoo5B/sandstack/internal/app/volume"
)

func registerSnapshotControls(router chi.Router, service *appvolume.Service) {
	const snapshotPath = "/volume/snapshots/{snapshotID}"
	finish := func(failed bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			err := service.FinishSnapshotCreation(chi.URLParam(r, "snapshotID"), failed)
			switch {
			case errors.Is(err, appvolume.ErrSnapshotNotFound):
				http.Error(w, "Snapshot not found", http.StatusNotFound)
			case errors.Is(err, appvolume.ErrSnapshotStateConflict):
				http.Error(w, "Snapshot creation cannot change to the requested outcome", http.StatusConflict)
			case err != nil:
				http.Error(w, "Unable to set the snapshot creation outcome", http.StatusInternalServerError)
			default:
				noContent(w, r)
			}
		}
	}
	router.Put(snapshotPath+"/complete-create", finish(false))
	router.Put(snapshotPath+"/fail-create", finish(true))
}
