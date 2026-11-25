package image

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	app "github.com/JSYoo5B/sandstack/internal/app/image"
)

const (
	imageListPath         = Path + "/images"
	defaultImagePageLimit = 25
	maxImagePageLimit     = 1000
)

type imageListBody struct {
	Images []imageBody `json:"images"`
	First  string      `json:"first"`
	Next   string      `json:"next,omitempty"`
	Schema string      `json:"schema"`
}

type imageListOptions struct {
	filter app.ListFilter
	marker *string
	limit  int
}

func (h handler) listImages(w http.ResponseWriter, r *http.Request, projectID string) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		http.Error(w, "The image list query is malformed.", http.StatusBadRequest)
		return
	}
	options, err := parseImageListQuery(query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	page, err := h.service.ListPage(projectID, options.filter, options.marker, options.limit)
	switch {
	case errors.Is(err, app.ErrImageNotFound):
		http.Error(w, "The image list marker could not be found.", http.StatusBadRequest)
		return
	case errors.Is(err, app.ErrImageMarkerForbidden):
		http.Error(w, "The image list marker is not accessible.", http.StatusForbidden)
		return
	case err != nil:
		http.Error(w, "Unable to list images.", http.StatusInternalServerError)
		return
	}

	query.Del("marker")
	first := imageListPath
	if encoded := query.Encode(); encoded != "" {
		first += "?" + encoded
	}
	response := imageListBody{
		Images: make([]imageBody, len(page.Images)),
		First:  first,
		Schema: imageListSchemaPath,
	}
	if page.NextMarker != "" {
		query.Set("marker", page.NextMarker)
		// Glance next links are relative to the Image endpoint's unversioned base.
		response.Next = strings.TrimPrefix(imageListPath, CatalogPath) + "?" + query.Encode()
	}
	for i, image := range page.Images {
		response.Images[i] = describeImage(image)
	}
	writeJSON(w, http.StatusOK, response)
}

func parseImageListQuery(query url.Values) (imageListOptions, error) {
	options := imageListOptions{limit: defaultImagePageLimit}
	for field, values := range query {
		if field == "tag" {
			for _, value := range values {
				options.filter.Tags = append(options.filter.Tags, strings.TrimSpace(value))
			}
			continue
		}
		if len(values) != 1 {
			return imageListOptions{}, errors.New("image list parameters accept one value each")
		}
		value := values[0]
		switch field {
		case "limit":
			limit, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || limit < 0 {
				return imageListOptions{}, errors.New("the image list limit must be a nonnegative integer")
			}
			options.limit = min(limit, maxImagePageLimit)
			continue
		case "marker":
			options.marker = &value
			continue
		}
		if operator, threshold, present := strings.Cut(value, ":"); present {
			if operator != "eq" {
				return imageListOptions{}, errors.New("image lists support only equality comparisons")
			}
			value = threshold
		}
		switch field {
		case "name":
			options.filter.Name = &value
		case "status":
			options.filter.Status = &value
		default:
			return imageListOptions{}, errors.New("the image list query contains an unsupported parameter")
		}
	}
	return options, nil
}
