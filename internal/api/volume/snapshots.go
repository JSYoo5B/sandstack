package volume

import (
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

type snapshotBody struct {
	ID          string            `json:"id"`
	VolumeID    string            `json:"volume_id"`
	Name        *string           `json:"name"`
	Description *string           `json:"description"`
	Size        int               `json:"size"`
	Status      string            `json:"status"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   *string           `json:"updated_at"`
	Metadata    map[string]string `json:"metadata"`
}

type snapshotDetailBody struct {
	snapshotBody
	ProjectID string `json:"os-extended-snapshot-attributes:project_id"`
	Progress  string `json:"os-extended-snapshot-attributes:progress"`
}

type snapshotResponse struct {
	Snapshot snapshotBody `json:"snapshot"`
}

func describeSnapshot(snapshot domain.Snapshot) snapshotBody {
	return snapshotBody{
		ID:          snapshot.ID,
		VolumeID:    snapshot.VolumeID,
		Name:        describeNullableText(snapshot.Name),
		Description: describeNullableText(snapshot.Description),
		Size:        snapshot.Size,
		Status:      string(snapshot.State),
		CreatedAt:   snapshot.CreatedAt.UTC().Format(timestampLayout),
		UpdatedAt:   optionalTimestamp(snapshot.UpdatedAt),
		Metadata:    snapshot.Metadata,
	}
}

func describeSnapshotDetail(snapshot domain.Snapshot) snapshotDetailBody {
	progress := "0%"
	if snapshot.State == domain.StateAvailable {
		progress = "100%"
	}
	return snapshotDetailBody{
		snapshotBody: describeSnapshot(snapshot),
		ProjectID:    snapshot.ProjectID,
		Progress:     progress,
	}
}

func (h handler) createSnapshot(w http.ResponseWriter, r *http.Request, projectID string) {
	data, ok := readJSONBody(w, r, "snapshot")
	if !ok {
		return
	}
	params, err := parseCreateSnapshot(data)
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}

	snapshot, err := h.service.CreateSnapshot(projectID, params.volumeID, app.SnapshotCreateOptions{
		Name:        params.name,
		Description: params.description,
		Force:       params.force,
		Metadata:    params.metadata,
	})
	switch {
	case errors.Is(err, app.ErrVolumeNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The source volume could not be found.")
	case errors.Is(err, app.ErrVolumeStateConflict):
		writeFault(w, http.StatusBadRequest, "badRequest", "The source volume must be available or in use with force enabled.")
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to create the snapshot.")
	default:
		writeJSON(w, http.StatusAccepted, snapshotResponse{Snapshot: describeSnapshot(snapshot)})
	}
}

func (h handler) listSnapshots(w http.ResponseWriter, r *http.Request, projectID string) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", "The snapshot list query is malformed.")
		return
	}
	options, err := parseDetailListQuery(query, "snapshot")
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}
	page, err := h.service.ListSnapshotsPage(projectID, options.filter, options.marker, options.limit)
	if errors.Is(err, app.ErrSnapshotNotFound) {
		writeFault(w, http.StatusNotFound, "itemNotFound", "The marker snapshot could not be found.")
		return
	}
	if err != nil {
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to list snapshots.")
		return
	}
	response := struct {
		Snapshots []snapshotDetailBody `json:"snapshots"`
		Links     []volumeLink         `json:"snapshots_links,omitempty"`
	}{
		Snapshots: make([]snapshotDetailBody, len(page.Snapshots)),
		Links:     h.detailListLinks(r, query, Path+"/"+projectID+"/snapshots/detail", page.NextMarker),
	}
	for i, snapshot := range page.Snapshots {
		response.Snapshots[i] = describeSnapshotDetail(snapshot)
	}
	writeJSON(w, http.StatusOK, response)
}

func (h handler) showSnapshot(w http.ResponseWriter, r *http.Request, projectID string) {
	snapshot, err := h.service.GetSnapshot(projectID, chi.URLParam(r, "snapshotID"))
	switch {
	case errors.Is(err, app.ErrSnapshotNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The snapshot could not be found.")
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to read the snapshot.")
	default:
		writeJSON(w, http.StatusOK, struct {
			Snapshot snapshotDetailBody `json:"snapshot"`
		}{Snapshot: describeSnapshotDetail(snapshot)})
	}
}

func (h handler) deleteSnapshot(w http.ResponseWriter, r *http.Request, projectID string) {
	var firstByte [1]byte
	if _, err := io.ReadFull(r.Body, firstByte[:]); err != io.EOF {
		writeFault(w, http.StatusBadRequest, "badRequest", "Snapshot deletion requires an empty request body.")
		return
	}

	err := h.service.DeleteSnapshot(projectID, chi.URLParam(r, "snapshotID"))
	switch {
	case errors.Is(err, app.ErrSnapshotNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The snapshot could not be found.")
	case errors.Is(err, app.ErrSnapshotStateConflict):
		writeFault(w, http.StatusBadRequest, "badRequest", "Snapshot deletion requires an available or error state.")
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to delete the snapshot.")
	default:
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusAccepted)
	}
}
