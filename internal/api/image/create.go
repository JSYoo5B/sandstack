package image

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"unicode/utf8"

	app "github.com/JSYoo5B/sandstack/internal/app/image"
)

const maxImageCreateBody = 64 * 1024

var (
	errImageCreate = errors.New("expected a named private image with optional bare container and qcow2 disk formats")
	errImagePublic = errors.New("the project member cannot create a public image")
)

func (h handler) createImage(w http.ResponseWriter, r *http.Request, projectID string) {
	if contentType := r.Header.Get("Content-Type"); contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || mediaType != "application/json" {
			http.Error(w, "Image creation requires application/json.", http.StatusUnsupportedMediaType)
			return
		}
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxImageCreateBody))
	var sizeError *http.MaxBytesError
	if errors.As(err, &sizeError) {
		http.Error(w, "The image create request is too large.", http.StatusRequestEntityTooLarge)
		return
	}
	if err != nil {
		http.Error(w, "Unable to read the image create request.", http.StatusBadRequest)
		return
	}
	options, err := parseImageCreate(data)
	switch {
	case errors.Is(err, errImagePublic):
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	case err != nil:
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	image, err := h.service.Create(projectID, options)
	switch {
	case errors.Is(err, app.ErrImageNotFound):
		http.Error(w, "The image project could not be found.", http.StatusNotFound)
	case err != nil:
		http.Error(w, "Unable to create the image.", http.StatusInternalServerError)
	default:
		body := describeImage(image)
		w.Header().Set("Location", h.endpointURL(r, body.Self))
		writeJSON(w, http.StatusCreated, body)
	}
}

func parseImageCreate(data []byte) (app.CreateOptions, error) {
	var fields map[string]json.RawMessage
	if !utf8.Valid(data) || json.Unmarshal(data, &fields) != nil || fields == nil {
		return app.CreateOptions{}, errImageCreate
	}
	var options app.CreateOptions
	var public bool
	for field, raw := range fields {
		var value *string
		if err := json.Unmarshal(raw, &value); err != nil {
			return app.CreateOptions{}, errImageCreate
		}
		switch field {
		case "name":
			if value == nil || *value == "" || utf8.RuneCountInString(*value) > maxImageTextLength {
				return app.CreateOptions{}, errImageCreate
			}
			options.Name = *value
		case "visibility":
			if value == nil || (*value != fixtureVisibility && *value != "public") {
				return app.CreateOptions{}, errImageCreate
			}
			public = *value == "public"
		case "container_format":
			if value != nil && *value != fixtureContainerFormat {
				return app.CreateOptions{}, errImageCreate
			}
			if value != nil {
				options.ContainerFormat = *value
			}
		case "disk_format":
			if value != nil && *value != fixtureDiskFormat {
				return app.CreateOptions{}, errImageCreate
			}
			if value != nil {
				options.DiskFormat = *value
			}
		default:
			return app.CreateOptions{}, errImageCreate
		}
	}
	if options.Name == "" {
		return app.CreateOptions{}, errImageCreate
	}
	if public {
		return app.CreateOptions{}, errImagePublic
	}
	return options, nil
}
