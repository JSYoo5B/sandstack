package volume

import "net/http"

type availabilityZoneBody struct {
	Name  string                    `json:"zoneName"`
	State availabilityZoneStateBody `json:"zoneState"`
}

type availabilityZoneStateBody struct {
	Available bool `json:"available"`
}

func (h handler) listAvailabilityZones(w http.ResponseWriter, _ *http.Request, _ string) {
	zones := h.service.ListAvailabilityZones()
	items := make([]availabilityZoneBody, len(zones))
	for i, zone := range zones {
		items[i] = availabilityZoneBody{
			Name:  zone.Name,
			State: availabilityZoneStateBody{Available: zone.Available},
		}
	}
	writeJSON(w, http.StatusOK, struct {
		Zones []availabilityZoneBody `json:"availabilityZoneInfo"`
	}{Zones: items})
}
