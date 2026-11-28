package volume

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
)

const (
	serviceRoot = "/volume"
	// Path is the versioned base path of the Block Storage service.
	Path = serviceRoot + "/v3"
)

type handler struct {
	service         *app.Service
	projectForToken func(string) (string, error)
	endpointURL     func(*http.Request, string) string
}

type projectHandler func(http.ResponseWriter, *http.Request, string)

// Register installs the supported project-qualified Block Storage operations.
func Register(
	router chi.Router,
	service *app.Service,
	projectForToken func(string) (string, error),
	endpointURL func(*http.Request, string) string,
) {
	h := handler{service: service, projectForToken: projectForToken, endpointURL: endpointURL}
	router.Get(Path+"/{projectID}/os-availability-zone", h.withProject(h.listAvailabilityZones))
	router.Get(Path+"/{projectID}/types", h.withProject(h.listVolumeTypes))
	router.Get(Path+"/{projectID}/types/{typeID}", h.withProject(withoutQuery(h.showVolumeType)))
	collection := Path + "/{projectID}/volumes"
	router.Get(collection+"/detail", h.withProject(h.listVolumes))
	router.Post(collection, h.withProject(withoutQuery(h.createVolume)))
	router.Get(collection+"/{volumeID}", h.withProject(withoutQuery(h.showVolume)))
	router.Put(collection+"/{volumeID}", h.withProject(withoutQuery(h.updateVolume)))
	router.Delete(collection+"/{volumeID}", h.withProject(withoutQuery(h.deleteVolume)))
	router.Post(collection+"/{volumeID}/action", h.withProject(withoutQuery(h.volumeAction)))
	volumeMetadata := collection + "/{volumeID}/metadata"
	router.Get(volumeMetadata, h.withProject(withoutQuery(h.showVolumeMetadata)))
	router.Put(volumeMetadata, h.withProject(withoutQuery(changeVolumeMetadata(service.ReplaceVolumeMetadata))))
	router.Post(volumeMetadata, h.withProject(withoutQuery(changeVolumeMetadata(service.MergeVolumeMetadata))))
	snapshots := Path + "/{projectID}/snapshots"
	router.Get(snapshots+"/detail", h.withProject(h.listSnapshots))
	router.Post(snapshots, h.withProject(withoutQuery(h.createSnapshot)))
	router.Get(snapshots+"/{snapshotID}", h.withProject(withoutQuery(h.showSnapshot)))
	router.Put(snapshots+"/{snapshotID}", h.withProject(withoutQuery(h.updateSnapshot)))
	router.Delete(snapshots+"/{snapshotID}", h.withProject(withoutQuery(h.deleteSnapshot)))
	backups := Path + "/{projectID}/backups"
	router.Get(backups, h.withProject(h.listBackups(false)))
	router.Get(backups+"/detail", h.withProject(h.listBackups(true)))
	router.Post(backups, h.withProject(withoutQuery(h.createBackup)))
	router.Get(backups+"/{backupID}", h.withProject(withoutQuery(h.showBackup)))
	router.Delete(backups+"/{backupID}", h.withProject(withoutQuery(h.deleteBackup)))
	router.Post(backups+"/{backupID}/restore", h.withProject(withoutQuery(h.restoreBackup)))
	transfers := Path + "/{projectID}/" + transferCollection
	router.Get(transfers, h.withProject(withoutQuery(h.listTransfers(false))))
	router.Get(transfers+"/detail", h.withProject(withoutQuery(h.listTransfers(true))))
	router.Post(transfers, h.withProject(withoutQuery(h.createTransfer)))
	router.Get(transfers+"/{transferID}", h.withProject(withoutQuery(h.showTransfer)))
	router.Delete(transfers+"/{transferID}", h.withProject(withoutQuery(h.deleteTransfer)))
	router.Post(transfers+"/{transferID}/accept", h.withProject(withoutQuery(h.acceptTransfer)))
	metadata := snapshots + "/{snapshotID}/metadata"
	router.Get(metadata, h.withProject(withoutQuery(h.showSnapshotMetadata)))
	router.Put(metadata, h.withProject(withoutQuery(changeSnapshotMetadata(service.ReplaceSnapshotMetadata))))
	router.Post(metadata, h.withProject(withoutQuery(changeSnapshotMetadata(service.MergeSnapshotMetadata))))
}

func (h handler) withProject(next projectHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokens := r.Header.Values("X-Auth-Token")
		if len(tokens) != 1 || tokens[0] == "" {
			writeUnauthorized(w)
			return
		}
		projectID, err := h.projectForToken(tokens[0])
		if err != nil || projectID == "" {
			writeUnauthorized(w)
			return
		}
		if !acceptVersion(w, r) {
			return
		}
		if chi.URLParam(r, "projectID") != projectID {
			writeFault(w, http.StatusBadRequest, "badRequest", "The URL project does not match the token's project.")
			return
		}
		next(w, r, projectID)
	}
}

func withoutQuery(next projectHandler) projectHandler {
	return func(w http.ResponseWriter, r *http.Request, projectID string) {
		if r.URL.RawQuery != "" {
			writeFault(w, http.StatusBadRequest, "badRequest", "Block Storage operations require an empty query string.")
			return
		}
		next(w, r, projectID)
	}
}

type httpFault struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Keystone")
	writeFault(w, http.StatusUnauthorized, "unauthorized", "A valid authentication token is required.")
}

func writeFault(w http.ResponseWriter, code int, kind, message string) {
	writeJSON(w, code, map[string]httpFault{kind: {Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
