package compute

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	appcompute "github.com/JSYoo5B/sandstack/internal/app/compute"
	domaincompute "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

const (
	computeRoot   = "/compute"
	powerRunning  = 1
	powerShutdown = 4
	powerCrashed  = 6
)

type serverResponse struct {
	Server serverBody `json:"server"`
}

type serverBody struct {
	serverCore
	VMState    string           `json:"OS-EXT-STS:vm_state"`
	PowerState int              `json:"OS-EXT-STS:power_state"`
	TaskState  *string          `json:"OS-EXT-STS:task_state"`
	Volumes    []attachedVolume `json:"os-extended-volumes:volumes_attached"`
}

type serverCore struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	ProjectID string            `json:"tenant_id"`
	UserID    string            `json:"user_id"`
	Created   time.Time         `json:"created"`
	Updated   time.Time         `json:"updated"`
	Status    string            `json:"status"`
	Fault     *instanceFault    `json:"fault,omitempty"`
	Links     []resourceLink    `json:"links"`
	Flavor    resourceRef       `json:"flavor"`
	Image     resourceRef       `json:"image"`
	Metadata  map[string]string `json:"metadata"`
	Addresses map[string]any    `json:"addresses"`
}

type attachedVolume struct {
	ID string `json:"id"`
}

type instanceFault struct {
	Code    int       `json:"code"`
	Message string    `json:"message"`
	Created time.Time `json:"created"`
}

type resourceLink struct {
	Rel  string `json:"rel"`
	Href string `json:"href"`
}

type resourceRef struct {
	ID    string         `json:"id"`
	Links []resourceLink `json:"links"`
}

func (h handler) showServer(w http.ResponseWriter, r *http.Request, scope requestScope) {
	server, err := h.service.Get(scope.projectID, chi.URLParam(r, "serverID"))
	if errors.Is(err, appcompute.ErrDetailUnavailable) {
		writeFault(w, http.StatusServiceUnavailable, "serviceUnavailable",
			"Server details are temporarily unavailable.")
		return
	}
	if errors.Is(err, appcompute.ErrServerNotFound) {
		writeFault(w, http.StatusNotFound, "itemNotFound", "The server could not be found.")
		return
	}
	if err != nil {
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to read the server.")
		return
	}

	writeJSON(w, http.StatusOK, serverResponse{Server: h.serverBody(r, server, scope.pathProject)})
}

func (h handler) serverBody(r *http.Request, server appcompute.ServerDetail, pathProject string) serverBody {
	volumes := make([]attachedVolume, len(server.AttachedVolumeIDs))
	for i, volumeID := range server.AttachedVolumeIDs {
		volumes[i] = attachedVolume{ID: volumeID}
	}
	status := server.Status()
	body := serverBody{
		serverCore: h.serverCore(r, server, pathProject),
		VMState:    string(status),
		PowerState: powerRunning,
		Volumes:    volumes,
	}
	switch status {
	case domaincompute.StatusShutoff:
		body.VMState = "stopped"
		body.PowerState = powerShutdown
	case domaincompute.StatusError:
		body.PowerState = powerCrashed
	}
	return body
}

func (h handler) serverCore(r *http.Request, server appcompute.ServerDetail, pathProject string) serverCore {
	projectPath := ""
	if pathProject != "" {
		projectPath = "/" + pathProject
	}
	serverPath := projectPath + "/servers/" + server.ID
	bookmark := computeRoot + projectPath
	status := server.Status()
	metadata := server.Metadata
	if metadata == nil {
		metadata = map[string]string{}
	}
	body := serverCore{
		ID:        server.ID,
		Name:      server.Name,
		ProjectID: server.ProjectID,
		UserID:    server.UserID,
		Created:   server.CreatedAt.UTC(),
		Updated:   server.UpdatedAt.UTC(),
		Status:    strings.ToUpper(string(status)),
		Links: []resourceLink{
			{Rel: "self", Href: h.endpointURL(r, Path+serverPath)},
			{Rel: "bookmark", Href: h.endpointURL(r, computeRoot+serverPath)},
		},
		Flavor: resourceRef{
			ID: appcompute.FixtureFlavorID,
			Links: []resourceLink{{
				Rel: "bookmark", Href: h.endpointURL(r, bookmark+"/flavors/"+appcompute.FixtureFlavorID),
			}},
		},
		Image: resourceRef{
			ID: server.ImageID,
			Links: []resourceLink{{
				Rel: "bookmark", Href: h.endpointURL(r, bookmark+"/images/"+server.ImageID),
			}},
		},
		Metadata:  metadata,
		Addresses: map[string]any{},
	}
	if status == domaincompute.StatusError {
		body.Fault = &instanceFault{
			Code:    server.Fault.Code,
			Message: server.Fault.Message,
			Created: server.Fault.CreatedAt.UTC(),
		}
	}
	return body
}
