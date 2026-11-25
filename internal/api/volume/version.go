package volume

import (
	"net/http"
	"regexp"
	"strings"
)

const (
	baselineVersion = "3.0"
	versionHeader   = "OpenStack-API-Version"
)

var microversionPattern = regexp.MustCompile(`^[1-9][0-9]*\.(0|[1-9][0-9]*)$`)

func acceptVersion(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Add("Vary", versionHeader)
	version, valid := requestedVersion(r.Header.Values(versionHeader))
	if !valid || (version != "" && version != "latest" && !microversionPattern.MatchString(version)) {
		writeFault(w, http.StatusBadRequest, "badRequest", "Invalid Block Storage API microversion.")
		return false
	}
	if version != "" && version != baselineVersion && version != "latest" {
		writeFault(w, http.StatusNotAcceptable, "computeFault", "This endpoint supports Block Storage API version 3.0.")
		return false
	}
	w.Header().Set(versionHeader, "volume "+baselineVersion)
	return true
}

func requestedVersion(values []string) (string, bool) {
	for _, value := range values {
		for _, entry := range strings.Split(value, ",") {
			fields := strings.Fields(entry)
			if len(fields) == 0 || fields[0] != "volume" {
				continue
			}
			if len(fields) != 2 {
				return "", false
			}
			return fields[1], true
		}
	}
	return "", len(values) == 0
}
