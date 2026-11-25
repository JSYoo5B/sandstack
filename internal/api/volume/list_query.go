package volume

import (
	"errors"
	"net/url"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

func parseVolumeListQuery(rawQuery string) (app.ListFilter, error) {
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return app.ListFilter{}, errors.New("the volume list query is malformed")
	}
	var filter app.ListFilter
	for field, values := range query {
		if len(values) != 1 {
			return app.ListFilter{}, errors.New("volume list filters accept one value per parameter")
		}
		switch field {
		case "name":
			filter.Name = &values[0]
		case "status":
			status := domain.State(values[0])
			filter.Status = &status
		default:
			return app.ListFilter{}, errors.New("volume lists accept only name and status query parameters")
		}
	}
	return filter, nil
}
