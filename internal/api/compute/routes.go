package compute

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	appcompute "github.com/JSYoo5B/sandstack/internal/app/compute"
)

// Path is the base path of the Compute v2.1 service.
const Path = "/compute/v2.1"

type handler struct {
	service         *appcompute.Service
	attachments     *appcompute.Attachments
	projectForToken func(string) (string, error)
	endpointURL     func(*http.Request, string) string
}

type requestScope struct {
	projectID   string
	pathProject string
}

// Register installs the supported Compute operations on the root router.
func Register(
	router chi.Router,
	service *appcompute.Service,
	attachments *appcompute.Attachments,
	projectForToken func(string) (string, error),
	endpointURL func(*http.Request, string) string,
) {
	h := handler{
		service: service, attachments: attachments, projectForToken: projectForToken, endpointURL: endpointURL,
	}
	show := h.withProject(h.showServer)
	update := h.withProject(h.updateServer)
	list := h.withProject(h.listServersDetailed)
	power := h.withProject(h.changeServerPower)
	showMetadata := h.withProject(h.showServerMetadata)
	replaceMetadata := h.withProject(changeServerMetadata(service.ReplaceMetadata))
	mergeMetadata := h.withProject(changeServerMetadata(service.MergeMetadata))
	listAttachments := h.withAttachmentProject(h.listAttachments)
	createAttachment := h.withAttachmentProject(h.createAttachment)
	showAttachment := h.withAttachmentProject(h.showAttachment)
	deleteAttachment := h.withAttachmentProject(h.deleteAttachment)
	for _, prefix := range []string{Path, Path + "/{projectID}"} {
		server := prefix + "/servers/{serverID}"
		router.Get(server, show)
		router.Put(server, update)
		router.Post(server+"/action", power)
		router.Get(server+"/metadata", showMetadata)
		router.Put(server+"/metadata", replaceMetadata)
		router.Post(server+"/metadata", mergeMetadata)
		router.Get(prefix+"/servers/detail", list)
		collection := server + "/os-volume_attachments"
		router.Get(collection, listAttachments)
		router.Post(collection, createAttachment)
		router.Get(collection+"/{volumeID}", showAttachment)
		router.Delete(collection+"/{volumeID}", deleteAttachment)
	}
}

func (h handler) withProject(next func(http.ResponseWriter, *http.Request, requestScope)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokens := r.Header.Values("X-Auth-Token")
		if len(tokens) != 1 || tokens[0] == "" {
			writeFault(w, http.StatusUnauthorized, "unauthorized", "A valid authentication token is required.")
			return
		}
		projectID, err := h.projectForToken(tokens[0])
		if err != nil || projectID == "" {
			writeFault(w, http.StatusUnauthorized, "unauthorized", "A valid authentication token is required.")
			return
		}
		if !acceptVersion(w, r) {
			return
		}

		pathProject := chi.URLParam(r, "projectID")
		if pathProject != "" && pathProject != projectID {
			writeFault(w, http.StatusBadRequest, "badRequest",
				"The URL project does not match the token's project.")
			return
		}
		next(w, r, requestScope{projectID: projectID, pathProject: pathProject})
	}
}

type httpFault struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func writeFault(w http.ResponseWriter, code int, kind string, message string) {
	writeJSON(w, code, map[string]httpFault{kind: {Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
