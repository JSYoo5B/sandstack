package compute

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"

	"github.com/go-chi/chi/v5"

	appcompute "github.com/JSYoo5B/sandstack/internal/app/compute"
)

type interfaceFixedIPBody struct {
	SubnetID  string `json:"subnet_id"`
	IPAddress string `json:"ip_address"`
}

type interfaceBody struct {
	PortState string                 `json:"port_state"`
	FixedIPs  []interfaceFixedIPBody `json:"fixed_ips"`
	PortID    string                 `json:"port_id"`
	NetID     string                 `json:"net_id"`
	MACAddr   string                 `json:"mac_addr"`
}

type interfaceResponse struct {
	Interface interfaceBody `json:"interfaceAttachment"`
}

func describeInterface(attached appcompute.ServerInterface) interfaceBody {
	body := interfaceBody{
		PortState: attached.PortState,
		FixedIPs:  make([]interfaceFixedIPBody, len(attached.FixedIPs)),
		PortID:    attached.PortID,
		NetID:     attached.NetworkID,
		MACAddr:   attached.MACAddress,
	}
	for i, fixedIP := range attached.FixedIPs {
		body.FixedIPs[i] = interfaceFixedIPBody{SubnetID: fixedIP.SubnetID, IPAddress: fixedIP.Address.String()}
	}
	return body
}

func (h handler) listInterfaces(w http.ResponseWriter, r *http.Request, scope requestScope) {
	interfaces, err := h.servers.ListInterfaces(scope.projectID, chi.URLParam(r, "serverID"))
	if err != nil {
		writeInterfaceError(w, err)
		return
	}
	response := struct {
		Interfaces []interfaceBody `json:"interfaceAttachments"`
	}{Interfaces: make([]interfaceBody, len(interfaces))}
	for i, attached := range interfaces {
		response.Interfaces[i] = describeInterface(attached)
	}
	writeJSON(w, http.StatusOK, response)
}

func (h handler) showInterface(w http.ResponseWriter, r *http.Request, scope requestScope) {
	attached, err := h.servers.GetInterface(scope.projectID, chi.URLParam(r, "serverID"), chi.URLParam(r, "portID"))
	if err != nil {
		writeInterfaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, interfaceResponse{Interface: describeInterface(attached)})
}

func (h handler) attachInterface(w http.ResponseWriter, r *http.Request, scope requestScope) {
	data, ok := readRequestBody(w, r, "interface attachment")
	if !ok {
		return
	}
	spec, err := parseInterfaceAttach(data)
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}

	attached, err := h.servers.AttachInterface(scope.actionRequest(), chi.URLParam(r, "serverID"), spec)
	if err != nil {
		writeInterfaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, interfaceResponse{Interface: describeInterface(attached)})
}

func (h handler) detachInterface(w http.ResponseWriter, r *http.Request, scope requestScope) {
	err := h.servers.DetachInterface(scope.actionRequest(), chi.URLParam(r, "serverID"), chi.URLParam(r, "portID"))
	if err != nil {
		writeInterfaceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusAccepted)
}

func writeInterfaceError(w http.ResponseWriter, err error) {
	var stateErr *appcompute.InvalidStateError
	var lockedErr *appcompute.LockedError
	var interfaceErr *appcompute.InterfaceError
	switch {
	case errors.Is(err, appcompute.ErrServerNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The server could not be found.")
	case errors.As(err, &lockedErr), errors.As(err, &stateErr):
		writeFault(w, http.StatusConflict, "conflictingRequest", err.Error())
	case !errors.As(err, &interfaceErr):
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to change the server's interfaces.")
	case errors.Is(err, appcompute.ErrPortNotFound), errors.Is(err, appcompute.ErrNetworkNotFound),
		errors.Is(err, appcompute.ErrInterfaceNotAttached):
		writeFault(w, http.StatusNotFound, "itemNotFound", interfaceErr.Message)
	case errors.Is(err, appcompute.ErrPortInUse), errors.Is(err, appcompute.ErrFixedIPInUse):
		writeFault(w, http.StatusConflict, "conflictingRequest", interfaceErr.Message)
	case errors.Is(err, appcompute.ErrExternalNetworkAttach):
		writeFault(w, http.StatusForbidden, "forbidden", interfaceErr.Message)
	default:
		writeFault(w, http.StatusBadRequest, "badRequest", interfaceErr.Message)
	}
}

func parseInterfaceAttach(data []byte) (appcompute.InterfaceAttach, error) {
	envelope, err := readUniqueObject(data)
	if err != nil {
		return appcompute.InterfaceAttach{}, errors.New("Invalid input for interface attachment: expected an object.")
	}
	for key := range envelope {
		if key != "interfaceAttachment" {
			return appcompute.InterfaceAttach{}, additionalPropertyError(key)
		}
	}
	raw, ok := envelope["interfaceAttachment"]
	if !ok {
		return appcompute.InterfaceAttach{}, nil
	}
	fields, err := readUniqueObject(raw)
	if err != nil {
		return appcompute.InterfaceAttach{}, errors.New("Invalid input for field 'interfaceAttachment'.")
	}
	var spec appcompute.InterfaceAttach
	for key, value := range fields {
		switch key {
		case "net_id":
			spec.NetworkID, err = parseInterfaceUUID(key, value)
		case "port_id":
			spec.PortID, err = parseInterfaceUUID(key, value)
		case "fixed_ips":
			spec.FixedIP, err = parseInterfaceFixedIPs(value)
		default:
			err = additionalPropertyError(key)
		}
		if err != nil {
			return appcompute.InterfaceAttach{}, err
		}
	}
	return spec, nil
}

func additionalPropertyError(key string) error {
	return errors.New("Additional properties are not allowed ('" + key + "' was unexpected).")
}

func parseInterfaceUUID(key string, value json.RawMessage) (string, error) {
	var id string
	if json.Unmarshal(value, &id) != nil || value[0] != '"' || !isUUIDLike(id) {
		return "", errors.New("Invalid input for field '" + key + "'.")
	}
	return id, nil
}

func parseInterfaceFixedIPs(value json.RawMessage) (*netip.Addr, error) {
	invalid := errors.New("Invalid input for field 'fixed_ips'.")
	var items []json.RawMessage
	if json.Unmarshal(value, &items) != nil || value[0] != '[' || len(items) != 1 {
		return nil, invalid
	}
	fields, err := readUniqueObject(items[0])
	if err != nil || len(fields) != 1 {
		return nil, invalid
	}
	raw, ok := fields["ip_address"]
	if !ok {
		return nil, invalid
	}
	var text string
	if json.Unmarshal(raw, &text) != nil || raw[0] != '"' {
		return nil, invalid
	}
	address, err := netip.ParseAddr(text)
	if err != nil || address.Zone() != "" {
		return nil, invalid
	}
	return &address, nil
}
