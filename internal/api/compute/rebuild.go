package compute

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"

	appcompute "github.com/JSYoo5B/sandstack/internal/app/compute"
)

var errInvalidRebuild = errors.New(
	"Invalid input for rebuild: expected an imageRef and optional name, adminPass, metadata, and OS-DCF:diskConfig.")

var unsupportedRebuildFields = []string{"preserve_ephemeral", "accessIPv4", "accessIPv6", "personality"}

var supportedRebuildFields = []string{"imageRef", "name", "adminPass", "metadata", diskConfigField}

type rebuiltServerBody struct {
	updatedServerBody
	AdminPass string `json:"adminPass"`
}

func (h handler) rebuildServer(w http.ResponseWriter, r *http.Request, scope requestScope, raw json.RawMessage) {
	spec, err := parseRebuild(raw)
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}

	rebuilt, err := h.servers.Rebuild(scope.actionRequest(), chi.URLParam(r, "serverID"), spec)
	var stateErr *appcompute.InvalidStateError
	var lockedErr *appcompute.LockedError
	var rebuildErr *appcompute.RebuildError
	switch {
	case errors.Is(err, appcompute.ErrServerNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The server could not be found.")
	case errors.As(err, &lockedErr), errors.As(err, &stateErr):
		writeFault(w, http.StatusConflict, "conflictingRequest", err.Error())
	case errors.Is(err, appcompute.ErrServerMetadataQuota):
		writeFault(w, http.StatusForbidden, "forbidden", err.Error())
	case errors.As(err, &rebuildErr):
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to rebuild the server.")
	default:
		progress := 0
		body := rebuiltServerBody{
			updatedServerBody: updatedServerBody{
				serverCore: h.serverCore(r, rebuilt.Server, scope.pathProject),
				DiskConfig: "MANUAL",
				Progress:   &progress,
			},
			AdminPass: rebuilt.AdminPass,
		}
		if rebuilt.Server.AutoDiskConfig {
			body.DiskConfig = "AUTO"
		}
		w.Header().Set("Location", body.Links[0].Href)
		writeJSON(w, http.StatusAccepted, struct {
			Server rebuiltServerBody `json:"server"`
		}{Server: body})
	}
}

func parseRebuild(raw json.RawMessage) (appcompute.ServerRebuild, error) {
	fields, err := readUniqueObject(raw)
	if err != nil {
		return appcompute.ServerRebuild{}, errInvalidRebuild
	}
	for key := range fields {
		if slices.Contains(unsupportedRebuildFields, key) {
			return appcompute.ServerRebuild{}, errors.New("The rebuild field '" + key + "' is not supported.")
		}
		if !slices.Contains(supportedRebuildFields, key) {
			return appcompute.ServerRebuild{}, errors.New(
				"Additional properties are not allowed ('" + key + "' was unexpected).")
		}
	}

	var spec appcompute.ServerRebuild
	value, ok := fields["imageRef"]
	if !ok {
		return appcompute.ServerRebuild{}, errors.New("'imageRef' is a required property.")
	}
	if json.Unmarshal(value, &spec.ImageID) != nil || !isUUIDLike(spec.ImageID) {
		return appcompute.ServerRebuild{}, errors.New("Invalid input for field 'imageRef'.")
	}
	if value, ok := fields["name"]; ok {
		var name string
		if json.Unmarshal(value, &name) != nil || !validServerName(name) {
			return appcompute.ServerRebuild{}, errors.New("Invalid input for field 'name'.")
		}
		spec.Name = &name
	}
	if value, ok := fields["adminPass"]; ok {
		if json.Unmarshal(value, &spec.AdminPass) != nil || spec.AdminPass == nil {
			return appcompute.ServerRebuild{}, errors.New("Invalid input for field 'adminPass'.")
		}
	}
	if value, ok := fields["metadata"]; ok {
		if spec.Metadata, err = parseMetadataObject(value); err != nil {
			return appcompute.ServerRebuild{}, errors.New("Invalid input for field 'metadata'.")
		}
	}
	if value, ok := fields[diskConfigField]; ok {
		var diskConfig string
		if json.Unmarshal(value, &diskConfig) != nil || (diskConfig != "AUTO" && diskConfig != "MANUAL") {
			return appcompute.ServerRebuild{}, errors.New("Invalid input for field '" + diskConfigField + "'.")
		}
		auto := diskConfig == "AUTO"
		spec.AutoDiskConfig = &auto
	}
	return spec, nil
}
