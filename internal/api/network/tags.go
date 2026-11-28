package network

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
)

const maxTagLength = 255

type tagResource struct {
	kind       app.TagResource
	path       string
	collection string
	member     string
}

var tagResources = []tagResource{
	{kind: app.TagNetwork, path: "/networks", collection: "networks", member: "network"},
	{kind: app.TagSubnet, path: "/subnets", collection: "subnets", member: "subnet"},
	{kind: app.TagPort, path: "/ports", collection: "ports", member: "port"},
	{kind: app.TagRouter, path: "/routers", collection: "routers", member: "router"},
	{kind: app.TagSecurityGroup, path: "/security-groups", collection: "security_groups", member: "security_group"},
	{kind: app.TagFloatingIP, path: "/floatingips", collection: "floatingips", member: "floatingip"},
}

func (h handler) registerTags(router chi.Router) {
	for _, resource := range tagResources {
		tags := Path + resource.path + "/{resourceID}/tags"
		router.Get(tags, h.withProject(withEmptyQuery(h.listTags(resource))))
		router.Put(tags, h.withProject(withEmptyQuery(h.replaceTags(resource))))
		router.Delete(tags, h.withProject(withEmptyQuery(h.clearTags(resource))))
		router.Get(tags+"/{tag}", h.withProject(withEmptyQuery(h.checkTag(resource))))
		router.Put(tags+"/{tag}", h.withProject(withEmptyQuery(h.addTag(resource))))
		router.Delete(tags+"/{tag}", h.withProject(withEmptyQuery(h.removeTag(resource))))
	}
}

func (h handler) listTags(resource tagResource) projectHandler {
	return func(w http.ResponseWriter, r *http.Request, projectID string) {
		tags, err := h.service.ResourceTags(projectID, resource.kind, chi.URLParam(r, "resourceID"))
		if err != nil {
			writeTagError(w, resource, "get", "", err)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Tags []string `json:"tags"`
		}{Tags: describeTags(tags)})
	}
}

func (h handler) replaceTags(resource tagResource) projectHandler {
	return func(w http.ResponseWriter, r *http.Request, projectID string) {
		data, ok := readCreateBody(w, r, "tags")
		if !ok {
			return
		}
		body, tags, err := parseTagsBody(data)
		if err != nil {
			writeInvalidTagInput(w, err.Error())
			return
		}
		err = h.service.ReplaceResourceTags(projectID, resource.kind, chi.URLParam(r, "resourceID"), tags)
		if err != nil {
			writeTagError(w, resource, "update", "", err)
			return
		}
		writeJSON(w, http.StatusOK, body)
	}
}

func (h handler) clearTags(resource tagResource) projectHandler {
	return func(w http.ResponseWriter, r *http.Request, projectID string) {
		if !requireEmptyDeleteBody(w, r) {
			return
		}
		if err := h.service.ClearResourceTags(projectID, resource.kind, chi.URLParam(r, "resourceID")); err != nil {
			writeTagError(w, resource, "delete", "", err)
			return
		}
		writeNoContent(w)
	}
}

func (h handler) checkTag(resource tagResource) projectHandler {
	return func(w http.ResponseWriter, r *http.Request, projectID string) {
		tag, ok := pathTag(w, r)
		if !ok {
			return
		}
		err := h.service.CheckResourceTag(projectID, resource.kind, chi.URLParam(r, "resourceID"), tag)
		if err != nil {
			writeTagError(w, resource, "get", tag, err)
			return
		}
		writeNoContent(w)
	}
}

func (h handler) addTag(resource tagResource) projectHandler {
	return func(w http.ResponseWriter, r *http.Request, projectID string) {
		tag, ok := pathTag(w, r)
		if !ok {
			return
		}
		err := h.service.AddResourceTag(projectID, resource.kind, chi.URLParam(r, "resourceID"), tag)
		if err != nil {
			writeTagError(w, resource, "update", tag, err)
			return
		}
		writeJSON(w, http.StatusCreated, nil)
	}
}

func (h handler) removeTag(resource tagResource) projectHandler {
	return func(w http.ResponseWriter, r *http.Request, projectID string) {
		if !requireEmptyDeleteBody(w, r) {
			return
		}
		tag, ok := pathTag(w, r)
		if !ok {
			return
		}
		err := h.service.RemoveResourceTag(projectID, resource.kind, chi.URLParam(r, "resourceID"), tag)
		if err != nil {
			writeTagError(w, resource, "delete", tag, err)
			return
		}
		writeNoContent(w)
	}
}

func pathTag(w http.ResponseWriter, r *http.Request) (string, bool) {
	tag := chi.URLParam(r, "tag")
	if r.URL.RawPath != "" {
		if unescaped, err := url.PathUnescape(tag); err == nil {
			tag = unescaped
		}
	}
	if utf8.RuneCountInString(tag) > maxTagLength {
		writeInvalidTagInput(w, fmt.Sprintf("'%s' exceeds maximum length of %d", tag, maxTagLength))
		return "", false
	}
	return tag, true
}

func parseTagsBody(data []byte) (map[string]json.RawMessage, []string, error) {
	invalidBody := errors.New("Invalid tags body")
	var body map[string]json.RawMessage
	if err := json.Unmarshal(data, &body); err != nil || body == nil {
		return nil, nil, invalidBody
	}
	value, ok := body["tags"]
	if !ok {
		return nil, nil, invalidBody
	}
	var items []json.RawMessage
	if err := json.Unmarshal(value, &items); err != nil || items == nil {
		return nil, nil, fmt.Errorf("'%s' is not a list", pythonText(value))
	}
	var duplicates []string
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		key := pythonRepr(item)
		if seen[key] && !slices.Contains(duplicates, pythonText(item)) {
			duplicates = append(duplicates, pythonText(item))
		}
		seen[key] = true
	}
	if len(duplicates) > 0 {
		return nil, nil, fmt.Errorf("Duplicate items in the list: '%s'", strings.Join(duplicates, ", "))
	}
	tags := make([]string, len(items))
	for i, item := range items {
		if json.Unmarshal(item, &tags[i]) != nil || string(item) == "null" {
			return nil, nil, fmt.Errorf("'%s' is not a valid string", pythonText(item))
		}
		if utf8.RuneCountInString(tags[i]) > maxTagLength {
			return nil, nil, fmt.Errorf("'%s' exceeds maximum length of %d", tags[i], maxTagLength)
		}
	}
	return body, tags, nil
}

func describeTags(tags []string) []string {
	if tags == nil {
		return []string{}
	}
	return tags
}

func writeInvalidTagInput(w http.ResponseWriter, message string) {
	writeError(w, http.StatusBadRequest, "InvalidInput", "Invalid input for operation: "+message+".")
}

func writeTagError(w http.ResponseWriter, resource tagResource, action, tag string, err error) {
	switch {
	case errors.Is(err, app.ErrTagResourceNotFound):
		writeError(w, http.StatusInternalServerError, "HTTPInternalServerError",
			"Request Failed: internal server error while processing your request.")
	case errors.Is(err, app.ErrTagResourceForbidden):
		writeError(w, http.StatusForbidden, "PolicyNotAuthorized",
			fmt.Sprintf("rule:%s_%s:tags is disallowed by policy", action, resource.member))
	case errors.Is(err, app.ErrTagNotFound):
		writeError(w, http.StatusNotFound, "TagNotFound", fmt.Sprintf("Tag %s could not be found.", tag))
	case errors.Is(err, app.ErrTagLimitExceeded):
		writeError(w, http.StatusBadRequest, "BadRequest", fmt.Sprintf(
			"Bad %s request: The number of tags exceed the per-resource limit of %d.",
			resource.collection, app.MaxResourceTags,
		))
	default:
		writeError(w, http.StatusInternalServerError, "HTTPInternalServerError", "Unable to process the tag request.")
	}
}
