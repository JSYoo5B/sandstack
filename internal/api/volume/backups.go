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

type backupSummaryBody struct {
	ID    string       `json:"id"`
	Name  *string      `json:"name"`
	Links []volumeLink `json:"links"`
}

type backupDetailBody struct {
	ID                  string       `json:"id"`
	Status              string       `json:"status"`
	Size                int          `json:"size"`
	ObjectCount         int          `json:"object_count"`
	AvailabilityZone    string       `json:"availability_zone"`
	Container           *string      `json:"container"`
	CreatedAt           string       `json:"created_at"`
	UpdatedAt           *string      `json:"updated_at"`
	Name                *string      `json:"name"`
	Description         *string      `json:"description"`
	FailReason          *string      `json:"fail_reason"`
	VolumeID            string       `json:"volume_id"`
	Links               []volumeLink `json:"links"`
	IsIncremental       bool         `json:"is_incremental"`
	HasDependentBackups bool         `json:"has_dependent_backups"`
	SnapshotID          *string      `json:"snapshot_id"`
	DataTimestamp       string       `json:"data_timestamp"`
}

func (h handler) backupLinks(r *http.Request, backup domain.Backup) []volumeLink {
	resourcePath := "/" + backup.ProjectID + "/backups/" + backup.ID
	return []volumeLink{
		{Href: h.endpointURL(r, Path+resourcePath), Rel: "self"},
		{Href: h.endpointURL(r, serviceRoot+resourcePath), Rel: "bookmark"},
	}
}

func (h handler) describeBackupSummary(r *http.Request, backup domain.Backup) backupSummaryBody {
	return backupSummaryBody{
		ID:    backup.ID,
		Name:  describeNullableText(backup.Name),
		Links: h.backupLinks(r, backup),
	}
}

func (h handler) describeBackupDetail(r *http.Request, backup domain.Backup) backupDetailBody {
	return backupDetailBody{
		ID:                  backup.ID,
		Status:              string(backup.State),
		Size:                backup.Size,
		AvailabilityZone:    app.StandardAvailabilityZone,
		Container:           describeNullableText(backup.Container),
		CreatedAt:           backup.CreatedAt.UTC().Format(timestampLayout),
		UpdatedAt:           optionalTimestamp(backup.UpdatedAt),
		Name:                describeNullableText(backup.Name),
		Description:         describeNullableText(backup.Description),
		FailReason:          optionalText(backup.FailReason),
		VolumeID:            backup.VolumeID,
		Links:               h.backupLinks(r, backup),
		IsIncremental:       backup.ParentID != "",
		HasDependentBackups: backup.DependentBackups > 0,
		SnapshotID:          optionalText(backup.SnapshotID),
		DataTimestamp:       backup.DataTimestamp.UTC().Format(timestampLayout),
	}
}

func (h handler) createBackup(w http.ResponseWriter, r *http.Request, projectID string) {
	data, ok := readJSONBody(w, r, "backup")
	if !ok {
		return
	}
	params, err := parseCreateBackup(data)
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}

	backup, err := h.service.CreateBackup(projectID, params.volumeID, params.options)
	switch {
	case errors.Is(err, app.ErrVolumeNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The volume could not be found.")
	case errors.Is(err, app.ErrSnapshotNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The snapshot could not be found.")
	case errors.Is(err, app.ErrSnapshotStateConflict):
		writeFault(w, http.StatusBadRequest, "badRequest", "The snapshot to back up must be available.")
	case errors.Is(err, app.ErrBackupVolumeState), errors.Is(err, app.ErrBackupSnapshotMismatch),
		errors.Is(err, app.ErrNoIncrementalParent):
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to create the backup.")
	default:
		writeJSON(w, http.StatusAccepted, struct {
			Backup backupSummaryBody `json:"backup"`
		}{Backup: h.describeBackupSummary(r, backup)})
	}
}

func (h handler) listBackups(detail bool) projectHandler {
	return func(w http.ResponseWriter, r *http.Request, projectID string) {
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			writeFault(w, http.StatusBadRequest, "badRequest", "The backup list query is malformed.")
			return
		}
		options, volumeID, err := parseBackupListQuery(query)
		if err != nil {
			writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
			return
		}
		filter := app.BackupFilter{Name: options.filter.Name, Status: options.filter.Status, VolumeID: volumeID}
		page, err := h.service.ListBackupsPage(projectID, filter, options.marker, options.limit)
		if errors.Is(err, app.ErrBackupNotFound) {
			writeFault(w, http.StatusNotFound, "itemNotFound", "The marker backup could not be found.")
			return
		}
		if err != nil {
			writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to list backups.")
			return
		}
		path := Path + "/" + projectID + "/backups"
		backups := make([]any, len(page.Backups))
		for i, backup := range page.Backups {
			if detail {
				backups[i] = h.describeBackupDetail(r, backup)
			} else {
				backups[i] = h.describeBackupSummary(r, backup)
			}
		}
		if detail {
			path += "/detail"
		}
		writeJSON(w, http.StatusOK, struct {
			Backups []any        `json:"backups"`
			Links   []volumeLink `json:"backups_links,omitempty"`
		}{Backups: backups, Links: h.detailListLinks(r, query, path, page.NextMarker)})
	}
}

func (h handler) showBackup(w http.ResponseWriter, r *http.Request, projectID string) {
	backup, err := h.service.GetBackup(projectID, chi.URLParam(r, "backupID"))
	switch {
	case errors.Is(err, app.ErrBackupNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The backup could not be found.")
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to read the backup.")
	default:
		writeJSON(w, http.StatusOK, struct {
			Backup backupDetailBody `json:"backup"`
		}{Backup: h.describeBackupDetail(r, backup)})
	}
}

func (h handler) deleteBackup(w http.ResponseWriter, r *http.Request, projectID string) {
	var firstByte [1]byte
	if _, err := io.ReadFull(r.Body, firstByte[:]); err != io.EOF {
		writeFault(w, http.StatusBadRequest, "badRequest", "Backup deletion requires an empty request body.")
		return
	}

	err := h.service.DeleteBackup(projectID, chi.URLParam(r, "backupID"))
	switch {
	case errors.Is(err, app.ErrBackupNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The backup could not be found.")
	case errors.Is(err, app.ErrBackupStateConflict):
		writeFault(w, http.StatusBadRequest, "badRequest", "Backup status must be available or error.")
	case errors.Is(err, app.ErrBackupHasDependents):
		writeFault(w, http.StatusBadRequest, "badRequest", "Incremental backups exist for this backup.")
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to delete the backup.")
	default:
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusAccepted)
	}
}

func (h handler) restoreBackup(w http.ResponseWriter, r *http.Request, projectID string) {
	data, ok := readJSONBody(w, r, "restore")
	if !ok {
		return
	}
	params, err := parseRestore(data)
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}

	restore, err := h.service.RestoreBackup(projectID, chi.URLParam(r, "backupID"), params.volumeID, params.name)
	switch {
	case errors.Is(err, app.ErrBackupNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The backup could not be found.")
	case errors.Is(err, app.ErrVolumeNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The volume could not be found.")
	case errors.Is(err, app.ErrBackupStateConflict):
		writeFault(w, http.StatusBadRequest, "badRequest", "Backup status must be available.")
	case errors.Is(err, app.ErrRestoreVolumeState), errors.Is(err, app.ErrRestoreVolumeTooSmall):
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to restore the backup.")
	default:
		writeJSON(w, http.StatusAccepted, map[string]any{"restore": map[string]any{
			"backup_id":   restore.BackupID,
			"volume_id":   restore.VolumeID,
			"volume_name": describeNullableText(restore.VolumeName),
		}})
	}
}
