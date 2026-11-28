package server

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	appvolume "github.com/JSYoo5B/sandstack/internal/app/volume"
)

func registerBackupControls(router chi.Router, service *appvolume.Service) {
	const backupPath = "/volume/backups/{backupID}"
	finish := func(apply func(string, bool) error, failed bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			err := apply(chi.URLParam(r, "backupID"), failed)
			switch {
			case errors.Is(err, appvolume.ErrBackupNotFound):
				http.Error(w, "Backup not found", http.StatusNotFound)
			case errors.Is(err, appvolume.ErrBackupCreationNotPending):
				http.Error(w, "Backup creation cannot change to the requested outcome", http.StatusConflict)
			case errors.Is(err, appvolume.ErrBackupRestoreNotPending):
				http.Error(w, "The backup has no pending restore", http.StatusConflict)
			case err != nil:
				http.Error(w, "Unable to set the backup outcome", http.StatusInternalServerError)
			default:
				noContent(w, r)
			}
		}
	}
	router.Put(backupPath+"/complete-create", finish(service.FinishBackupCreation, false))
	router.Put(backupPath+"/fail-create", finish(service.FinishBackupCreation, true))
	router.Put(backupPath+"/complete-restore", finish(service.FinishBackupRestore, false))
	router.Put(backupPath+"/fail-restore", finish(service.FinishBackupRestore, true))
}
