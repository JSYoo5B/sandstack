package compute

import (
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
	imagedomain "github.com/JSYoo5B/sandstack/internal/domain/image"
	networkdomain "github.com/JSYoo5B/sandstack/internal/domain/network"
	volumedomain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

var (
	ErrImageNotFound          = errors.New("can not find requested image")
	ErrImageNotActive         = errors.New("image is not active")
	ErrSecurityGroupNotFound  = errors.New("security group not found")
	ErrSecurityGroupAmbiguous = errors.New("security group name matches more than one group")
	ErrNetworkNotFound        = errors.New("network not found")
	ErrNetworkRequiresSubnet  = errors.New("network requires a subnet")
	ErrNetworkAmbiguous       = errors.New("more than one network is available")
	ErrPortNotFound           = errors.New("port not found")
	ErrPortInUse              = errors.New("port is in use")
	ErrPortRequiresFixedIP    = errors.New("port requires a fixed IP")
	ErrFixedIPInUse           = errors.New("fixed IP is already in use")
	ErrServerIDGeneration     = errors.New("server ID generation failed")
)

// CreateError carries Nova's message for a rejected server creation.
type CreateError struct {
	Kind    error
	Message string
}

func (e *CreateError) Error() string { return e.Message }

func (e *CreateError) Unwrap() error { return e.Kind }

func createError(kind error, format string, args ...any) error {
	return &CreateError{Kind: kind, Message: fmt.Sprintf(format, args...)}
}

type serverImages interface {
	Get(projectID, imageID string) (imagedomain.Image, error)
	snapshotImages
}

type serverNetworks interface {
	Get(projectID, networkID string) (networkdomain.Network, error)
	AvailableNetworks(projectID string) []networkdomain.Network
	ProjectSecurityGroups(projectID string) []networkdomain.SecurityGroup
	GetPort(projectID, portID string) (networkdomain.Port, error)
	CreateDevicePort(
		projectID, networkID string, address *netip.Addr, securityGroups *[]string, deviceID, deviceOwner string,
	) (networkdomain.Port, error)
	BindPort(projectID, portID, deviceID, deviceOwner string) (networkdomain.Port, error)
	UnbindPort(projectID, portID, deviceID string) error
	DeletePort(projectID, portID string) error
	DescribePort(projectID, portID string) (networkdomain.DeviceInterface, error)
	SetPortSecurityGroups(projectID, portID string, groupIDs []string) (networkdomain.Port, error)
	DevicePorts(projectID, deviceID string) []networkdomain.Port
	AddressDevice(projectID, networkID string, address netip.Addr) (string, bool)
}

type serverVolumes interface {
	attachmentLookup
	Lookup(projectID, volumeID string) (volumedomain.Volume, error)
	ReserveRoot(projectID, serverID, volumeID string) (volumedomain.Attachment, error)
	CompleteReservation(projectID, volumeID string) error
	CreateServerVolume(projectID, imageID string, size int) (volumedomain.Volume, error)
	CompleteCreation(volumeID string) error
	Detach(projectID, serverID, volumeID string) error
	Delete(projectID, volumeID string) error
	CreateServerSnapshot(projectID, volumeID, name string) (volumedomain.Snapshot, error)
}

// ServerCreate is a validated server creation request. A nil Networks
// selects the project's only available network.
type ServerCreate struct {
	Name           string
	ImageID        string
	FlavorID       string
	KeyName        string
	AdminPass      string
	AutoDiskConfig bool
	Metadata       map[string]string
	Networks       []NetworkRequest
	SecurityGroups []string
	BlockDevices   []BlockDevice
	GroupID        string
}

// NetworkRequest selects either an existing port or a network for a new port.
type NetworkRequest struct {
	NetworkID string
	PortID    string
	FixedIP   *netip.Addr
}

type CreatedServer struct {
	Server    domain.Server
	AdminPass string
}

// ServerNetwork lists the server's addresses on one network label.
type ServerNetwork struct {
	Label     string
	Addresses []ServerAddress
}

type ServerAddress struct {
	Address  netip.Addr
	Floating bool
	MAC      string
}

// Servers orchestrates server creation, deletion, and reads across Glance,
// Neutron, and Cinder.
type Servers struct {
	compute     *Service
	images      serverImages
	networks    serverNetworks
	volumes     serverVolumes
	keyPairs    *KeyPairs
	newID       func() string
	newPassword func() string
}

func NewServers(
	compute *Service,
	images serverImages,
	networks serverNetworks,
	volumes serverVolumes,
	keyPairs *KeyPairs,
	newID, newPassword func() string,
) *Servers {
	return &Servers{
		compute: compute, images: images, networks: networks, volumes: volumes, keyPairs: keyPairs,
		newID: newID, newPassword: newPassword,
	}
}

type plannedPort struct {
	networkID string
	portID    string
	address   *netip.Addr
	external  bool
	hasSubnet bool
}

// Create validates the request in Nova's order and records the server with
// its ports. Failures Nova reports after accepting the request leave the
// server in ERROR with a fault.
func (s *Servers) Create(request ActionRequest, spec ServerCreate) (CreatedServer, error) {
	projectID := request.ProjectID
	flavor, err := s.compute.GetFlavor(projectID, spec.FlavorID)
	if err != nil {
		return CreatedServer{}, createError(ErrFlavorNotFound, "Flavor %s could not be found.", spec.FlavorID)
	}
	source, err := s.bootSource(projectID, spec)
	if err != nil {
		return CreatedServer{}, err
	}
	groupIDs, err := s.resolveSecurityGroups(projectID, spec.SecurityGroups)
	if err != nil {
		return CreatedServer{}, err
	}
	plan, err := s.planNetworks(projectID, spec.Networks)
	if err != nil {
		return CreatedServer{}, err
	}
	if spec.KeyName != "" {
		if _, err := s.keyPairs.Get(request.UserID, spec.KeyName); err != nil {
			return CreatedServer{}, createError(ErrKeyPairNotFound, "Invalid key_name provided.")
		}
	}
	if len(spec.Metadata) > maxMetadataItems {
		return CreatedServer{}, createError(ErrServerMetadataQuota,
			"Maximum number of metadata items exceeds %d", maxMetadataItems)
	}
	if err := checkBootImage(source); err != nil {
		return CreatedServer{}, err
	}
	if err := s.requestedGroup(projectID, spec.GroupID); err != nil {
		return CreatedServer{}, err
	}
	if len(spec.BlockDevices) > 0 {
		if err := s.validateBlockDevices(projectID, spec.BlockDevices); err != nil {
			return CreatedServer{}, err
		}
	}
	if err := s.checkGroupMemberQuota(request, spec.GroupID); err != nil {
		return CreatedServer{}, err
	}
	bootVolume := newBootVolume(spec.BlockDevices)
	imageMeta := s.bootImageMeta(projectID, spec, source, flavor.DiskGiB)

	serverID, err := s.allocateServerID()
	if err != nil {
		return CreatedServer{}, err
	}
	if err := s.reserveBootVolume(projectID, serverID, bootVolume); err != nil {
		return CreatedServer{}, err
	}
	s.compute.mu.RLock()
	held := s.compute.holdBuilds
	unscheduled := !held && s.compute.groupHostTaken(projectID, spec.GroupID, serverID)
	s.compute.mu.RUnlock()
	var ports []domain.ServerPort
	var failure string
	if !unscheduled {
		var err error
		ports, failure, err = s.allocatePorts(projectID, serverID, plan, groupIDs)
		if err != nil {
			if bootVolume != nil && bootVolume.VolumeID != "" {
				_ = s.volumes.Detach(projectID, serverID, bootVolume.VolumeID)
			}
			return CreatedServer{}, err
		}
	}
	switch {
	case unscheduled:
	case failure != "":
		s.releaseBootVolume(projectID, serverID, bootVolume)
	case !held:
		if failure = s.prepareBootVolume(projectID, serverID, bootVolume); failure != "" {
			s.releasePorts(projectID, serverID, ports)
			ports = nil
		}
	}
	password := spec.AdminPass
	if password == "" {
		password = s.newPassword()
	}

	s.compute.mu.Lock()
	defer s.compute.mu.Unlock()
	now := s.compute.now().UTC()
	server := domain.Server{
		ID:             serverID,
		Name:           spec.Name,
		ProjectID:      projectID,
		UserID:         request.UserID,
		ImageID:        source.imageID,
		FlavorID:       flavor.ID,
		KeyName:        spec.KeyName,
		AutoDiskConfig: spec.AutoDiskConfig,
		CreatedAt:      now,
		UpdatedAt:      now,
		State:          domain.StatusActive,
		Metadata:       copyMetadata(spec.Metadata),
		Ports:          ports,
		BootVolume:     bootVolume,
		ImageMeta:      imageMeta,
	}
	switch {
	case unscheduled:
		server.State = domain.StatusError
		server.Fault = &domain.Fault{Code: 500, Message: buildFailureMessage, CreatedAt: now}
	case failure != "":
		server.State = domain.StatusError
		server.Fault = &domain.Fault{
			Code:      500,
			Message:   fmt.Sprintf("Build of instance %s aborted: %s", serverID, failure),
			CreatedAt: now,
		}
	case held:
		server.State = domain.StatusBuild
	default:
		server.Placed = true
	}
	record := &serverRecord{server: server, console: []byte{}}
	s.compute.servers[serverID] = record
	s.compute.joinGroup(projectID, spec.GroupID, serverID)
	s.compute.recordAction(record, request, domain.ActionCreate)
	return CreatedServer{Server: cloneServer(server), AdminPass: password}, nil
}

func (s *Servers) bootSource(projectID string, spec ServerCreate) (bootSource, error) {
	if spec.ImageID == "" {
		return s.volumeBootSource(projectID, spec.BlockDevices)
	}
	image, err := s.images.Get(projectID, spec.ImageID)
	if err != nil {
		return bootSource{}, createError(ErrImageNotFound, "Can not find requested image")
	}
	if _, ok := image.Properties["block_device_mapping"]; ok {
		return bootSource{}, createError(ErrImageBlockDevices,
			"Booting from an image with a block device mapping is not supported.")
	}
	return bootSource{imageID: image.ID, label: image.ID, status: image.Status()}, nil
}

func (s *Servers) allocateServerID() (string, error) {
	s.compute.mu.RLock()
	defer s.compute.mu.RUnlock()

	for range 8 {
		id := s.newID()
		if _, exists := s.compute.servers[id]; id != "" && !exists {
			return id, nil
		}
	}
	return "", ErrServerIDGeneration
}

func (s *Servers) resolveSecurityGroups(projectID string, requested []string) ([]string, error) {
	if len(requested) == 0 {
		return nil, nil
	}
	groups := s.networks.ProjectSecurityGroups(projectID)
	ids := make([]string, 0, len(requested))
	for _, name := range requested {
		var matches []string
		for _, group := range groups {
			if group.ID == name {
				matches = []string{group.ID}
				break
			}
			if group.Name == name {
				matches = append(matches, group.ID)
			}
		}
		switch len(matches) {
		case 0:
			return nil, createError(ErrSecurityGroupNotFound, "Security group %s not found.", name)
		case 1:
			if !slices.Contains(ids, matches[0]) {
				ids = append(ids, matches[0])
			}
		default:
			return nil, createError(ErrSecurityGroupAmbiguous,
				"Multiple security_group matches found for name '%s', use an ID to be more specific.", name)
		}
	}
	return ids, nil
}

func (s *Servers) planNetworks(projectID string, requests []NetworkRequest) ([]plannedPort, error) {
	if len(requests) == 0 {
		available := s.networks.AvailableNetworks(projectID)
		switch len(available) {
		case 0:
			return nil, nil
		case 1:
			return []plannedPort{{
				networkID: available[0].ID,
				hasSubnet: len(available[0].SubnetIDs) > 0,
			}}, nil
		default:
			return nil, createError(ErrNetworkAmbiguous,
				"Multiple possible networks found, use a Network ID to be more specific.")
		}
	}

	plan := make([]plannedPort, 0, len(requests))
	for _, request := range requests {
		if request.PortID == "" {
			plan = append(plan, plannedPort{networkID: request.NetworkID, address: request.FixedIP})
			continue
		}
		port, err := s.networks.GetPort(projectID, request.PortID)
		if err != nil {
			return nil, createError(ErrPortNotFound, "Port id %s could not be found.", request.PortID)
		}
		if port.DeviceID != "" {
			return nil, createError(ErrPortInUse, "Port %s is still in use.", request.PortID)
		}
		if !port.FixedIP.IsValid() {
			return nil, createError(ErrPortRequiresFixedIP,
				"Port %s requires a FixedIP in order to be used.", request.PortID)
		}
		plan = append(plan, plannedPort{networkID: port.NetworkID, portID: port.ID, hasSubnet: true})
	}

	for _, planned := range plan {
		if planned.address == nil {
			continue
		}
		if deviceID, used := s.networks.AddressDevice(projectID, planned.networkID, *planned.address); used {
			return nil, createError(ErrFixedIPInUse,
				"Fixed IP address %s is already in use on instance %s.", planned.address, deviceID)
		}
	}

	var missing []string
	for i := range plan {
		if plan[i].portID != "" {
			continue
		}
		network, err := s.networks.Get(projectID, plan[i].networkID)
		if err != nil {
			if !slices.Contains(missing, plan[i].networkID) {
				missing = append(missing, plan[i].networkID)
			}
			continue
		}
		if len(network.SubnetIDs) == 0 {
			return nil, createError(ErrNetworkRequiresSubnet,
				"Network %s requires a subnet in order to boot instances on.", network.ID)
		}
		plan[i].hasSubnet = true
		plan[i].external = network.External && network.ProjectID != projectID
	}
	if len(missing) > 0 {
		return nil, createError(ErrNetworkNotFound, "Network %s could not be found.", strings.Join(missing, ", "))
	}
	return plan, nil
}

// allocatePorts binds requested ports and creates the others. It returns a
// build failure reason, or an error that rejects the request.
func (s *Servers) allocatePorts(
	projectID, serverID string, plan []plannedPort, groupIDs []string,
) ([]domain.ServerPort, string, error) {
	var ports []domain.ServerPort
	rollback := func() {
		s.releasePorts(projectID, serverID, ports)
		ports = nil
	}
	for _, planned := range plan {
		if planned.external {
			rollback()
			return nil, fmt.Sprintf("It is not allowed to create an interface on external network %s",
				planned.networkID), nil
		}
		if planned.portID != "" {
			if _, err := s.networks.BindPort(projectID, planned.portID, serverID,
				networkdomain.DeviceOwnerCompute); err != nil {
				rollback()
				return nil, "", createError(ErrPortInUse, "Port %s is still in use.", planned.portID)
			}
			ports = append(ports, domain.ServerPort{ID: planned.portID, Preserve: true})
			continue
		}
		if !planned.hasSubnet {
			rollback()
			return nil, "Network requires port_security_enabled and subnet associated in order to apply " +
				"security groups.", nil
		}
		var groups *[]string
		if groupIDs != nil {
			groups = &groupIDs
		}
		port, err := s.networks.CreateDevicePort(
			projectID, planned.networkID, planned.address, groups, serverID, networkdomain.DeviceOwnerCompute)
		if err != nil {
			rollback()
			return nil, err.Error(), nil
		}
		ports = append(ports, domain.ServerPort{ID: port.ID})
	}
	return ports, "", nil
}

func (s *Servers) releasePorts(projectID, serverID string, ports []domain.ServerPort) {
	for _, port := range ports {
		if port.Preserve {
			_ = s.networks.UnbindPort(projectID, port.ID, serverID)
		} else {
			_ = s.networks.DeletePort(projectID, port.ID)
		}
	}
}

// Delete removes an unlocked server in any state, detaches its volumes,
// unbinds the ports it was given, and deletes the ports it created.
func (s *Servers) Delete(request ActionRequest, serverID string) error {
	projectID := request.ProjectID
	s.compute.mu.Lock()
	defer s.compute.mu.Unlock()

	record, ok := s.compute.find(projectID, serverID)
	if !ok {
		return ErrServerNotFound
	}
	if record.server.Locked {
		return ErrServerLocked
	}
	delete(s.compute.servers, serverID)
	s.compute.leaveGroups(serverID)
	if record.server.PendingImageID != "" {
		_ = s.images.Delete(projectID, record.server.PendingImageID)
	}
	bootVolume := record.server.BootVolume
	for _, attachment := range s.volumes.ListAttachments(projectID, serverID) {
		if bootVolume != nil && bootVolume.VolumeID == attachment.VolumeID {
			s.releaseBootVolume(projectID, serverID, bootVolume)
			continue
		}
		_ = s.volumes.Detach(projectID, serverID, attachment.VolumeID)
	}
	s.releasePorts(projectID, serverID, record.server.Ports)
	return nil
}

// Get reads the server detail and adds its live network view.
func (s *Servers) Get(projectID, serverID string) (ServerDetail, error) {
	detail, err := s.compute.Get(projectID, serverID)
	if err != nil {
		return ServerDetail{}, err
	}
	return s.describe(detail), nil
}

func (s *Servers) List(projectID string, filter ListFilter) []ServerDetail {
	details := s.compute.List(projectID, filter)
	for i := range details {
		details[i] = s.describe(details[i])
	}
	return details
}

func (s *Servers) UpdateName(projectID, serverID string, name *string) (ServerDetail, error) {
	detail, err := s.compute.UpdateName(projectID, serverID, name)
	if err != nil {
		return ServerDetail{}, err
	}
	return s.describe(detail), nil
}

// describe hides addresses while the server builds, as Nova does.
func (s *Servers) describe(detail ServerDetail) ServerDetail {
	building := detail.Status() == domain.StatusBuild
	fixed := map[string][]ServerAddress{}
	floating := map[string][]ServerAddress{}
	var labels []string
	for _, port := range detail.Ports {
		described, err := s.networks.DescribePort(detail.ProjectID, port.ID)
		if err != nil {
			continue
		}
		detail.SecurityGroups = append(detail.SecurityGroups, described.SecurityGroupNames...)
		if building {
			continue
		}
		label := described.NetworkName
		if !slices.Contains(labels, label) {
			labels = append(labels, label)
		}
		mac := described.Port.MACAddress
		if described.Port.FixedIP.IsValid() {
			fixed[label] = append(fixed[label], ServerAddress{Address: described.Port.FixedIP, MAC: mac})
		}
		for _, address := range described.FloatingAddresses {
			floating[label] = append(floating[label], ServerAddress{Address: address, Floating: true, MAC: mac})
		}
	}
	for _, label := range labels {
		detail.Networks = append(detail.Networks, ServerNetwork{
			Label:     label,
			Addresses: append(fixed[label], floating[label]...),
		})
	}
	return detail
}
