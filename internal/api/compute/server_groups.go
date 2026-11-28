package compute

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	appcompute "github.com/JSYoo5B/sandstack/internal/app/compute"
	domaincompute "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

const maxServerGroupPage = 1000

var nonNegativeInteger = regexp.MustCompile(`^[0-9]+$`)

type serverGroupBody struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Policies []string          `json:"policies"`
	Members  []string          `json:"members"`
	Metadata map[string]string `json:"metadata"`
}

type serverGroupResponse struct {
	ServerGroup serverGroupBody `json:"server_group"`
}

func formatServerGroup(group domaincompute.ServerGroup) serverGroupBody {
	return serverGroupBody{
		ID:       group.ID,
		Name:     group.Name,
		Policies: []string{group.Policy},
		Members:  group.Members,
		Metadata: map[string]string{},
	}
}

func (h handler) createServerGroup(w http.ResponseWriter, r *http.Request, scope requestScope) {
	data, ok := readRequestBody(w, r, "server group")
	if !ok {
		return
	}
	name, policy, err := parseServerGroupRequest(data)
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}
	group, err := h.serverGroups.Create(scope.actionRequest(), name, policy)
	if err != nil {
		writeServerGroupError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, serverGroupResponse{ServerGroup: formatServerGroup(group)})
}

func (h handler) listServerGroups(w http.ResponseWriter, r *http.Request, scope requestScope) {
	offset, limit, err := serverGroupPage(r.URL.Query())
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}
	groups := h.serverGroups.List(scope.projectID)
	offset = min(offset, len(groups))
	groups = groups[offset:min(len(groups), offset+limit)]
	response := struct {
		ServerGroups []serverGroupBody `json:"server_groups"`
	}{ServerGroups: make([]serverGroupBody, len(groups))}
	for i, group := range groups {
		response.ServerGroups[i] = formatServerGroup(group)
	}
	writeJSON(w, http.StatusOK, response)
}

func (h handler) showServerGroup(w http.ResponseWriter, r *http.Request, scope requestScope) {
	groupID := pathParam(r, "serverGroupID")
	group, err := h.serverGroups.Get(scope.projectID, groupID)
	if err != nil {
		writeServerGroupError(w, err, groupID)
		return
	}
	writeJSON(w, http.StatusOK, serverGroupResponse{ServerGroup: formatServerGroup(group)})
}

func (h handler) deleteServerGroup(w http.ResponseWriter, r *http.Request, scope requestScope) {
	groupID := pathParam(r, "serverGroupID")
	if err := h.serverGroups.Delete(scope.projectID, groupID); err != nil {
		writeServerGroupError(w, err, groupID)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func writeServerGroupError(w http.ResponseWriter, err error, groupID string) {
	switch {
	case errors.Is(err, appcompute.ErrServerGroupNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "Instance group "+groupID+" could not be found.")
	case errors.Is(err, appcompute.ErrServerGroupQuota):
		writeFault(w, http.StatusForbidden, "forbidden", err.Error())
	default:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to process the server group request.")
	}
}

func parseServerGroupRequest(data []byte) (string, string, error) {
	envelope, err := readUniqueObject(data)
	if err != nil {
		return "", "", errors.New("Expected a JSON object with a server_group object.")
	}
	for key := range envelope {
		if key != "server_group" {
			return "", "", errors.New("Additional properties are not allowed ('" + key + "' was unexpected).")
		}
	}
	raw, ok := envelope["server_group"]
	if !ok {
		return "", "", errors.New("'server_group' is a required property.")
	}
	fields, err := readUniqueObject(raw)
	if err != nil {
		return "", "", errors.New("Invalid input for field 'server_group'.")
	}
	for key := range fields {
		if key != "name" && key != "policies" {
			return "", "", errors.New("Additional properties are not allowed ('" + key + "' was unexpected).")
		}
	}
	name, err := parseNameField(fields, "name", true)
	if err != nil {
		return "", "", err
	}
	policies, ok := fields["policies"]
	if !ok {
		return "", "", errors.New("'policies' is a required property.")
	}
	var items []json.RawMessage
	if json.Unmarshal(policies, &items) != nil || len(items) != 1 {
		return "", "", errors.New("Invalid input for field 'policies'.")
	}
	var policy string
	if json.Unmarshal(items[0], &policy) != nil || items[0][0] != '"' ||
		(policy != domaincompute.PolicyAffinity && policy != domaincompute.PolicyAntiAffinity) {
		return "", "", errors.New("Invalid input for field 'policies'.")
	}
	return name, policy, nil
}

func serverGroupPage(query url.Values) (int, int, error) {
	offset, limit := 0, maxServerGroupPage
	for _, field := range []string{"offset", "limit"} {
		values := query[field]
		for _, value := range values {
			if !nonNegativeInteger.MatchString(value) {
				return 0, 0, errors.New("Invalid input for query parameters " + field + ".")
			}
		}
		if len(values) == 0 {
			continue
		}
		number, err := strconv.Atoi(values[len(values)-1])
		if err != nil {
			number = math.MaxInt
		}
		if field == "offset" {
			offset = number
		} else if number > 0 {
			limit = min(number, maxServerGroupPage)
		}
	}
	return offset, limit, nil
}
