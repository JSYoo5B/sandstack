package volume

import (
	"errors"
	"net/url"
	"strconv"
	"strings"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

const maxVolumePageLimit = 1000

type volumeListOptions struct {
	filter app.ListFilter
	marker *string
	limit  int
}

func parseVolumeListQuery(query url.Values) (volumeListOptions, error) {
	options := volumeListOptions{limit: maxVolumePageLimit}
	for field, values := range query {
		if len(values) != 1 {
			return volumeListOptions{}, errors.New("volume list parameters accept one value each")
		}
		switch field {
		case "name":
			options.filter.Name = &values[0]
		case "status":
			status := domain.State(values[0])
			options.filter.Status = &status
		case "marker":
			options.marker = &values[0]
		case "limit":
			limit, err := strconv.Atoi(strings.TrimSpace(values[0]))
			if err != nil || limit < 0 {
				return volumeListOptions{}, errors.New("the volume list limit must be a nonnegative integer")
			}
			options.limit = min(limit, maxVolumePageLimit)
		case "sort":
			if values[0] != "id:asc" {
				return volumeListOptions{}, errors.New("the volume list supports only sort=id:asc")
			}
		default:
			return volumeListOptions{}, errors.New("the volume list query contains an unsupported parameter")
		}
	}
	return options, nil
}
