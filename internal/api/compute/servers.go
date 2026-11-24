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
	fixtureImage  = "962a0750-d2e0-4400-88e0-a9ad35758eac"
	fixtureFlavor = "1"
	powerRunning  = 1
	powerCrashed  = 6
)

type serverResponse struct {
	Server serverBody `json:"server"`
}

type serverBody struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	ProjectID  string            `json:"tenant_id"`
	UserID     string            `json:"user_id"`
	Created    time.Time         `json:"created"`
	Updated    time.Time         `json:"updated"`
	Status     string            `json:"status"`
	VMState    string            `json:"OS-EXT-STS:vm_state"`
	PowerState int               `json:"OS-EXT-STS:power_state"`
	TaskState  *string           `json:"OS-EXT-STS:task_state"`
	Fault      *instanceFault    `json:"fault,omitempty"`
	Links      []resourceLink    `json:"links"`
	Flavor     resourceRef       `json:"flavor"`
	Image      resourceRef       `json:"image"`
	Metadata   map[string]string `json:"metadata"`
	Addresses  map[string]any    `json:"addresses"`
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

func (h handler) serverBody(r *http.Request, server domaincompute.Server, pathProject string) serverBody {
	projectPath := ""
	if pathProject != "" {
		projectPath = "/" + pathProject
	}
	serverPath := projectPath + "/servers/" + server.ID
	bookmark := computeRoot + projectPath
	status := string(server.Status())
	body := serverBody{
		ID:         server.ID,
		Name:       server.Name,
		ProjectID:  server.ProjectID,
		UserID:     server.UserID,
		Created:    server.CreatedAt.UTC(),
		Updated:    server.UpdatedAt.UTC(),
		Status:     strings.ToUpper(status),
		VMState:    status,
		PowerState: powerRunning,
		Links: []resourceLink{
			{Rel: "self", Href: h.endpointURL(r, Path+serverPath)},
			{Rel: "bookmark", Href: h.endpointURL(r, computeRoot+serverPath)},
		},
		Flavor: resourceRef{
			ID: fixtureFlavor,
			Links: []resourceLink{{
				Rel: "bookmark", Href: h.endpointURL(r, bookmark+"/flavors/"+fixtureFlavor),
			}},
		},
		Image: resourceRef{
			ID: fixtureImage,
			Links: []resourceLink{{
				Rel: "bookmark", Href: h.endpointURL(r, bookmark+"/images/"+fixtureImage),
			}},
		},
		Metadata:  map[string]string{},
		Addresses: map[string]any{},
	}
	if server.Fault != nil {
		body.PowerState = powerCrashed
		body.Fault = &instanceFault{
			Code:    server.Fault.Code,
			Message: server.Fault.Message,
			Created: server.Fault.CreatedAt.UTC(),
		}
	}
	return body
}
