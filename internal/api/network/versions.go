package network

import "net/http"

type version struct {
	ID     string        `json:"id"`
	Status string        `json:"status"`
	Links  []versionLink `json:"links"`
}

type versionLink struct {
	Href string `json:"href"`
	Rel  string `json:"rel"`
}

func (h handler) listVersions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, struct {
		Versions []version `json:"versions"`
	}{Versions: []version{{
		ID:     "v2.0",
		Status: "CURRENT",
		Links:  []versionLink{{Href: h.endpointURL(r, Path), Rel: "self"}},
	}}})
}
