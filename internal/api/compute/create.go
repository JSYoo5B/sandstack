package compute

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	appcompute "github.com/JSYoo5B/sandstack/internal/app/compute"
)

const maxUserData = 65535

var unsupportedCreateFields = []string{
	"personality", "availability_zone", "block_device_mapping",
	"config_drive", "accessIPv4", "accessIPv6", "return_reservation_id",
}

var schedulerHintFields = []string{"os:scheduler_hints", "OS-SCH-HNT:scheduler_hints"}

var supportedCreateFields = []string{
	"name", "imageRef", "flavorRef", "adminPass", "metadata", "networks", "key_name",
	"security_groups", "OS-DCF:diskConfig", "user_data", "min_count", "max_count", blockDeviceField,
}

type createdServerBody struct {
	ID             string            `json:"id"`
	Links          []resourceLink    `json:"links"`
	DiskConfig     string            `json:"OS-DCF:diskConfig"`
	SecurityGroups []json.RawMessage `json:"security_groups"`
	AdminPass      string            `json:"adminPass"`
}

type badCreateRequest string

func (e badCreateRequest) Error() string { return string(e) }

func (h handler) createServer(w http.ResponseWriter, r *http.Request, scope requestScope) {
	data, ok := readRequestBody(w, r, "server create")
	if !ok {
		return
	}
	spec, securityGroups, err := parseServerCreate(data)
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}
	created, err := h.servers.Create(scope.actionRequest(), spec)
	if err != nil {
		writeCreateError(w, err)
		return
	}

	links := h.serverLinks(r, created.Server.ID, computeProjectPath(scope.pathProject))
	body := createdServerBody{
		ID:             created.Server.ID,
		Links:          links,
		DiskConfig:     "MANUAL",
		SecurityGroups: securityGroups,
		AdminPass:      created.AdminPass,
	}
	if created.Server.AutoDiskConfig {
		body.DiskConfig = "AUTO"
	}
	w.Header().Set("Location", links[0].Href)
	writeJSON(w, http.StatusAccepted, struct {
		Server createdServerBody `json:"server"`
	}{Server: body})
}

func writeCreateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, appcompute.ErrSecurityGroupAmbiguous), errors.Is(err, appcompute.ErrNetworkAmbiguous),
		errors.Is(err, appcompute.ErrPortInUse):
		writeFault(w, http.StatusConflict, "conflictingRequest", err.Error())
	case errors.Is(err, appcompute.ErrServerMetadataQuota), errors.Is(err, appcompute.ErrServerGroupMemberQuota):
		writeFault(w, http.StatusForbidden, "forbidden", err.Error())
	case errors.As(err, new(*appcompute.CreateError)):
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
	default:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to create the server.")
	}
}

func (h handler) deleteServer(w http.ResponseWriter, r *http.Request, scope requestScope) {
	err := h.servers.Delete(scope.actionRequest(), chi.URLParam(r, "serverID"))
	switch {
	case errors.Is(err, appcompute.ErrServerNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "Instance could not be found")
	case errors.Is(err, appcompute.ErrServerLocked):
		writeFault(w, http.StatusConflict, "conflictingRequest", err.Error())
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to delete the server.")
	default:
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	}
}

// parseServerCreate applies the v2.1 create schema and then Nova's API
// checks for imageRef and networks. It returns the security_groups items to
// echo in the response.
func parseServerCreate(data []byte) (appcompute.ServerCreate, []json.RawMessage, error) {
	envelope, err := readUniqueObject(data)
	if err != nil || !utf8.Valid(data) {
		return appcompute.ServerCreate{}, nil, badCreateRequest("Expected a JSON object with a server object.")
	}
	for key := range envelope {
		if key != "server" && !slices.Contains(schedulerHintFields, key) {
			return appcompute.ServerCreate{}, nil, badCreateRequest("Additional properties are not allowed ('" + key +
				"' was unexpected).")
		}
	}
	groupID, err := parseSchedulerHints(envelope)
	if err != nil {
		return appcompute.ServerCreate{}, nil, err
	}
	fields, err := readUniqueObject(envelope["server"])
	if err != nil {
		return appcompute.ServerCreate{}, nil, badCreateRequest("Expected a JSON object with a server object.")
	}
	for key := range fields {
		if slices.Contains(unsupportedCreateFields, key) {
			return appcompute.ServerCreate{}, nil, badCreateRequest("The server field '" + key + "' is not supported.")
		}
		if !slices.Contains(supportedCreateFields, key) {
			return appcompute.ServerCreate{}, nil, badCreateRequest("Additional properties are not allowed ('" + key +
				"' was unexpected).")
		}
	}

	spec := appcompute.ServerCreate{GroupID: groupID}
	if spec.Name, err = parseNameField(fields, "name", true); err != nil {
		return appcompute.ServerCreate{}, nil, err
	}
	if spec.FlavorID, err = parseFlavorRef(fields["flavorRef"]); err != nil {
		return appcompute.ServerCreate{}, nil, err
	}
	imageRef, err := parseImageRef(fields["imageRef"])
	if err != nil {
		return appcompute.ServerCreate{}, nil, err
	}
	if raw, ok := fields["adminPass"]; ok && json.Unmarshal(raw, &spec.AdminPass) != nil {
		return appcompute.ServerCreate{}, nil, badCreateRequest("Invalid input for field 'adminPass'.")
	}
	if raw, ok := fields["metadata"]; ok {
		if spec.Metadata, err = parseMetadataObject(raw); err != nil {
			return appcompute.ServerCreate{}, nil, badCreateRequest("Invalid input for field 'metadata'.")
		}
	}
	if spec.KeyName, err = parseNameField(fields, "key_name", false); err != nil {
		return appcompute.ServerCreate{}, nil, err
	}
	securityGroups, err := parseSecurityGroups(fields["security_groups"], &spec)
	if err != nil {
		return appcompute.ServerCreate{}, nil, err
	}
	if raw, ok := fields["OS-DCF:diskConfig"]; ok {
		var diskConfig string
		if json.Unmarshal(raw, &diskConfig) != nil || (diskConfig != "AUTO" && diskConfig != "MANUAL") {
			return appcompute.ServerCreate{}, nil, badCreateRequest("Invalid input for field 'OS-DCF:diskConfig'.")
		}
		spec.AutoDiskConfig = diskConfig == "AUTO"
	}
	if err := checkUserData(fields["user_data"]); err != nil {
		return appcompute.ServerCreate{}, nil, err
	}
	if err := checkCounts(fields["min_count"], fields["max_count"]); err != nil {
		return appcompute.ServerCreate{}, nil, err
	}
	networks, err := parseNetworkItems(fields["networks"])
	if err != nil {
		return appcompute.ServerCreate{}, nil, err
	}
	blockDevices, err := parseBlockDeviceItems(fields[blockDeviceField])
	if err != nil {
		return appcompute.ServerCreate{}, nil, err
	}

	if spec.BlockDevices, err = requestedBlockDevices(blockDevices, imageRef); err != nil {
		return appcompute.ServerCreate{}, nil, err
	}
	if imageRef == "" && len(spec.BlockDevices) == 0 {
		return appcompute.ServerCreate{}, nil, badCreateRequest("Missing imageRef attribute")
	}
	spec.ImageID = imageRef
	if spec.Networks, err = requestedNetworks(networks); err != nil {
		return appcompute.ServerCreate{}, nil, err
	}
	return spec, securityGroups, nil
}

func parseNameField(fields map[string]json.RawMessage, field string, required bool) (string, error) {
	raw, ok := fields[field]
	if !ok {
		if required {
			return "", badCreateRequest("'" + field + "' is a required property.")
		}
		return "", nil
	}
	var name string
	if json.Unmarshal(raw, &name) != nil || !validServerName(name) {
		return "", badCreateRequest("Invalid input for field '" + field + "'.")
	}
	return name, nil
}

func parseFlavorRef(raw json.RawMessage) (string, error) {
	if raw == nil {
		return "", badCreateRequest("'flavorRef' is a required property.")
	}
	var ref string
	if json.Unmarshal(raw, &ref) != nil {
		var number json.Number
		if json.Unmarshal(raw, &number) != nil {
			return "", badCreateRequest("Invalid input for field 'flavorRef'.")
		}
		if _, err := strconv.ParseInt(number.String(), 10, 64); err != nil {
			return "", badCreateRequest("Invalid input for field 'flavorRef'.")
		}
		ref = number.String()
	}
	if ref == "" {
		return "", badCreateRequest("Invalid input for field 'flavorRef'.")
	}
	parsed, err := url.Parse(ref)
	path := ref
	if err == nil {
		path = parsed.Path
	}
	return path[strings.LastIndex(path, "/")+1:], nil
}

func parseImageRef(raw json.RawMessage) (string, error) {
	if raw == nil {
		return "", nil
	}
	var ref string
	if json.Unmarshal(raw, &ref) != nil || (ref != "" && !isUUIDLike(ref)) {
		return "", badCreateRequest("Invalid input for field 'imageRef'.")
	}
	return ref, nil
}

func parseSecurityGroups(raw json.RawMessage, spec *appcompute.ServerCreate) ([]json.RawMessage, error) {
	if raw == nil {
		return []json.RawMessage{json.RawMessage(`{"name":"default"}`)}, nil
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil || items == nil {
		return nil, badCreateRequest("Invalid input for field 'security_groups'.")
	}
	for _, item := range items {
		group, err := readUniqueObject(item)
		if err != nil || len(group) > 1 || (len(group) == 1 && group["name"] == nil) {
			return nil, badCreateRequest("Invalid input for field 'security_groups'.")
		}
		name, err := parseNameField(group, "name", false)
		if err != nil {
			return nil, badCreateRequest("Invalid input for field 'security_groups'.")
		}
		if name != "" && !slices.Contains(spec.SecurityGroups, name) {
			spec.SecurityGroups = append(spec.SecurityGroups, name)
		}
	}
	return items, nil
}

func checkUserData(raw json.RawMessage) error {
	if raw == nil {
		return nil
	}
	var userData string
	if json.Unmarshal(raw, &userData) != nil || len(userData) > maxUserData {
		return badCreateRequest("Invalid input for field 'user_data'.")
	}
	if _, err := base64.StdEncoding.DecodeString(userData); err != nil {
		return badCreateRequest("Invalid input for field 'user_data'.")
	}
	return nil
}

func checkCounts(minRaw, maxRaw json.RawMessage) error {
	minCount, err := parsePositiveInteger(minRaw, "min_count")
	if err != nil {
		return err
	}
	maxCount, err := parsePositiveInteger(maxRaw, "max_count")
	if err != nil {
		return err
	}
	if maxRaw == nil {
		maxCount = minCount
	}
	if minCount > maxCount {
		return badCreateRequest("min_count must be <= max_count")
	}
	if maxCount > 1 {
		return badCreateRequest("Creating more than one server per request is not supported.")
	}
	return nil
}

func parsePositiveInteger(raw json.RawMessage, field string) (int64, error) {
	if raw == nil {
		return 1, nil
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		var number json.Number
		if json.Unmarshal(raw, &number) != nil {
			return 0, badCreateRequest("Invalid input for field '" + field + "'.")
		}
		text = number.String()
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil || value < 1 || strings.ContainsAny(text, "+-") {
		return 0, badCreateRequest("Invalid input for field '" + field + "'.")
	}
	return value, nil
}

type networkItem struct {
	uuid    *string
	port    *string
	fixedIP *netip.Addr
}

func parseNetworkItems(raw json.RawMessage) ([]networkItem, error) {
	if raw == nil {
		return nil, nil
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil || items == nil {
		return nil, badCreateRequest("Invalid input for field 'networks'.")
	}
	networks := make([]networkItem, 0, len(items))
	for _, item := range items {
		fields, err := readUniqueObject(item)
		if err != nil {
			return nil, badCreateRequest("Invalid input for field 'networks'.")
		}
		var network networkItem
		for key, value := range fields {
			var text *string
			if json.Unmarshal(value, &text) != nil {
				return nil, badCreateRequest("Invalid input for field 'networks'.")
			}
			switch {
			case key == "uuid" && text != nil:
				network.uuid = text
			case key == "port" && (text == nil || isUUIDLike(*text)):
				network.port = text
			case key == "fixed_ip" && text != nil:
				address, err := netip.ParseAddr(*text)
				if err != nil || address.Zone() != "" {
					return nil, badCreateRequest("Invalid input for field 'networks'.")
				}
				network.fixedIP = &address
			default:
				return nil, badCreateRequest("Invalid input for field 'networks'.")
			}
		}
		networks = append(networks, network)
	}
	return networks, nil
}

func requestedNetworks(items []networkItem) ([]appcompute.NetworkRequest, error) {
	if items == nil {
		return nil, nil
	}
	requests := make([]appcompute.NetworkRequest, 0, len(items))
	var ports []string
	for _, item := range items {
		if item.port != nil && *item.port != "" {
			port := *item.port
			if slices.Contains(ports, port) {
				return nil, badCreateRequest("Port ID '" + port +
					"' was specified twice: you cannot attach a port multiple times.")
			}
			if item.fixedIP != nil {
				return nil, badCreateRequest("Specified Fixed IP '" + item.fixedIP.String() +
					"' cannot be used with port '" + port + "': the two cannot be specified together.")
			}
			ports = append(ports, port)
			requests = append(requests, appcompute.NetworkRequest{PortID: port})
			continue
		}
		if item.uuid == nil {
			return nil, badCreateRequest("Bad network format: missing 'uuid'")
		}
		if !isUUIDLike(*item.uuid) {
			return nil, badCreateRequest("Bad networks format: network uuid is not in proper format (" +
				*item.uuid + ")")
		}
		requests = append(requests, appcompute.NetworkRequest{NetworkID: *item.uuid, FixedIP: item.fixedIP})
	}
	return requests, nil
}

// isUUIDLike follows oslo.utils, which ignores case, hyphens, braces, and URN prefixes.
func isUUIDLike(value string) bool {
	normalized := strings.NewReplacer("urn:", "", "uuid:", "", "-", "").Replace(value)
	normalized = strings.Trim(normalized, "{}")
	if len(normalized) != 32 {
		return false
	}
	for _, char := range normalized {
		if !strings.ContainsRune("0123456789abcdefABCDEF", char) {
			return false
		}
	}
	return true
}

func parseSchedulerHints(envelope map[string]json.RawMessage) (string, error) {
	groups := make(map[string]string, len(schedulerHintFields))
	for _, field := range schedulerHintFields {
		raw, ok := envelope[field]
		if !ok {
			continue
		}
		hints, err := readUniqueObject(raw)
		if err != nil {
			return "", badCreateRequest("Invalid input for field '" + field + "'.")
		}
		for key, value := range hints {
			if key != "group" {
				return "", badCreateRequest("The scheduler hint '" + key + "' is not supported.")
			}
			if len(value) == 0 || value[0] != '"' {
				return "", badCreateRequest("The scheduler hint 'group' must be a single server group ID.")
			}
			var group string
			if json.Unmarshal(value, &group) != nil || !isUUIDLike(group) {
				return "", badCreateRequest("Invalid input for field 'group'.")
			}
			groups[field] = group
		}
	}
	if _, ok := envelope[schedulerHintFields[0]]; ok {
		return groups[schedulerHintFields[0]], nil
	}
	return groups[schedulerHintFields[1]], nil
}
