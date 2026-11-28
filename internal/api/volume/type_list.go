package volume

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
)

type volumeTypesResponse struct {
	VolumeTypes []volumeTypeBody `json:"volume_types"`
	Links       []volumeLink     `json:"volume_type_links,omitempty"`
}

func (h handler) listVolumeTypes(w http.ResponseWriter, r *http.Request, projectID string) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", "The volume type list query is malformed.")
		return
	}

	marker, limit, err := parseVolumeTypeList(query)
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}
	page, err := h.service.ListVolumeTypesPage(marker, limit)
	switch {
	case errors.Is(err, app.ErrVolumeTypeNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The volume type marker could not be found.")
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to list the volume types.")
	default:
		response := volumeTypesResponse{
			VolumeTypes: make([]volumeTypeBody, len(page.Types)),
			Links:       h.detailListLinks(r, query, Path+"/"+projectID+"/types", page.NextMarker),
		}
		for i, volumeType := range page.Types {
			response.VolumeTypes[i] = describeVolumeType(volumeType, true)
		}
		writeJSON(w, http.StatusOK, response)
	}
}

func parseVolumeTypeList(query url.Values) (*string, int, error) {
	var marker *string
	limit := maxDetailPageLimit
	for field, values := range query {
		if len(values) != 1 {
			return nil, 0, errors.New("volume type list parameters accept one value each")
		}
		switch field {
		case "is_public", "name", "description":
			// Baseline Cinder applies the project member's visibility and ignores these filters.
		case "marker":
			marker = &values[0]
		case "limit":
			value, err := strconv.Atoi(strings.TrimSpace(values[0]))
			if err != nil || value < 0 {
				return nil, 0, errors.New("the volume type list limit must be a nonnegative integer")
			}
			limit = min(value, maxDetailPageLimit)
		default:
			return nil, 0, errors.New("the volume type list query contains an unsupported parameter")
		}
	}
	return marker, limit, nil
}
