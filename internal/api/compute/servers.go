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
	powerNoState  = 0
	powerRunning  = 1
	powerPaused   = 3
	powerShutdown = 4
	powerCrashed  = 6
)

type serverResponse struct {
	Server serverBody `json:"server"`
}

type serverBody struct {
	serverCore
	KeyName        *string             `json:"key_name"`
	SecurityGroups []securityGroupName `json:"security_groups,omitempty"`
	VMState        string              `json:"OS-EXT-STS:vm_state"`
	PowerState     int                 `json:"OS-EXT-STS:power_state"`
	TaskState      *string             `json:"OS-EXT-STS:task_state"`
	Volumes        []attachedVolume    `json:"os-extended-volumes:volumes_attached"`
}

type serverCore struct {
	ID        string                     `json:"id"`
	Name      string                     `json:"name"`
	ProjectID string                     `json:"tenant_id"`
	UserID    string                     `json:"user_id"`
	Created   time.Time                  `json:"created"`
	Updated   time.Time                  `json:"updated"`
	Status    string                     `json:"status"`
	Fault     *instanceFault             `json:"fault,omitempty"`
	Links     []resourceLink             `json:"links"`
	Flavor    resourceRef                `json:"flavor"`
	Image     any                        `json:"image"`
	Metadata  map[string]string          `json:"metadata"`
	Addresses map[string][]serverAddress `json:"addresses"`
}

type securityGroupName struct {
	Name string `json:"name"`
}

type serverAddress struct {
	Version int    `json:"version"`
	Address string `json:"addr"`
	Type    string `json:"OS-EXT-IPS:type,omitempty"`
	MAC     string `json:"OS-EXT-IPS-MAC:mac_addr,omitempty"`
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
	server, err := h.servers.Get(scope.projectID, chi.URLParam(r, "serverID"))
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
	body.Addresses = serverAddresses(server.Networks, true)
	if server.KeyName != "" {
		keyName := server.KeyName
		body.KeyName = &keyName
	}
	for _, name := range server.SecurityGroups {
		body.SecurityGroups = append(body.SecurityGroups, securityGroupName{Name: name})
	}
	switch status {
	case domaincompute.StatusShutoff:
		body.VMState = "stopped"
		body.PowerState = powerShutdown
	case domaincompute.StatusSuspended:
		body.PowerState = powerShutdown
	case domaincompute.StatusPaused:
		body.PowerState = powerPaused
	case domaincompute.StatusError:
		body.PowerState = powerCrashed
	case domaincompute.StatusResized:
		body.VMState = "resized"
		if server.Resize != nil && server.Resize.State == domaincompute.StatusShutoff {
			body.PowerState = powerShutdown
		}
	case domaincompute.StatusBuild:
		taskState := "scheduling"
		body.VMState = "building"
		body.PowerState = powerNoState
		body.TaskState = &taskState
	}
	if server.TaskState != "" && status != domaincompute.StatusBuild {
		taskState := server.TaskState
		body.TaskState = &taskState
	}
	return body
}

func (h handler) serverCore(r *http.Request, server appcompute.ServerDetail, pathProject string) serverCore {
	projectPath := computeProjectPath(pathProject)
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
		Links:     h.serverLinks(r, server.ID, projectPath),
		Flavor: resourceRef{
			ID: server.FlavorID,
			Links: []resourceLink{{
				Rel: "bookmark", Href: h.endpointURL(r, bookmark+"/flavors/"+server.FlavorID),
			}},
		},
		Image:     "",
		Metadata:  metadata,
		Addresses: serverAddresses(server.Networks, false),
	}
	if server.ImageID != "" {
		body.Image = resourceRef{
			ID: server.ImageID,
			Links: []resourceLink{{
				Rel: "bookmark", Href: h.endpointURL(r, bookmark+"/images/"+server.ImageID),
			}},
		}
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

func computeProjectPath(projectID string) string {
	if projectID == "" {
		return ""
	}
	return "/" + projectID
}

func (h handler) serverLinks(r *http.Request, serverID, projectPath string) []resourceLink {
	resourcePath := projectPath + "/servers/" + serverID
	return []resourceLink{
		{Rel: "self", Href: h.endpointURL(r, Path+resourcePath)},
		{Rel: "bookmark", Href: h.endpointURL(r, computeRoot+resourcePath)},
	}
}

func serverAddresses(networks []appcompute.ServerNetwork, extended bool) map[string][]serverAddress {
	addresses := make(map[string][]serverAddress, len(networks))
	for _, network := range networks {
		entries := make([]serverAddress, len(network.Addresses))
		for i, address := range network.Addresses {
			entries[i] = serverAddress{Version: 4, Address: address.Address.String()}
			if address.Address.Is6() {
				entries[i].Version = 6
			}
			if extended {
				entries[i].Type = "fixed"
				entries[i].MAC = address.MAC
			}
			if extended && address.Floating {
				entries[i].Type = "floating"
			}
		}
		addresses[network.Label] = entries
	}
	return addresses
}
