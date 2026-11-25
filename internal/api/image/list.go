package image

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	app "github.com/JSYoo5B/sandstack/internal/app/image"
)

type imageListBody struct {
	Images []imageBody `json:"images"`
	First  string      `json:"first"`
	Schema string      `json:"schema"`
}

func (h handler) listImages(w http.ResponseWriter, r *http.Request, projectID string) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		http.Error(w, "The image list query is malformed.", http.StatusBadRequest)
		return
	}
	filter, err := parseListFilter(query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	first := Path + "/images"
	if encoded := query.Encode(); encoded != "" {
		first += "?" + encoded
	}
	images := h.service.List(projectID, filter)
	response := imageListBody{
		Images: make([]imageBody, len(images)),
		First:  first,
		Schema: Path + "/schemas/images",
	}
	for i, image := range images {
		response.Images[i] = describeImage(image)
	}
	writeJSON(w, http.StatusOK, response)
}

func parseListFilter(query url.Values) (app.ListFilter, error) {
	var filter app.ListFilter
	for field, values := range query {
		if len(values) != 1 {
			return app.ListFilter{}, errors.New("image list filters accept one value per parameter")
		}
		value := values[0]
		if operator, threshold, present := strings.Cut(value, ":"); present {
			if operator != "eq" {
				return app.ListFilter{}, errors.New("image lists support only equality comparisons")
			}
			value = threshold
		}
		switch field {
		case "name":
			filter.Name = &value
		case "status":
			filter.Status = &value
		default:
			return app.ListFilter{}, errors.New("image lists accept only name and status query parameters")
		}
	}
	return filter, nil
}
