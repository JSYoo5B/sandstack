package compute

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"

	networkdomain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

var (
	ErrSecurityGroupCannotBeApplied = errors.New("security group cannot be applied")
	ErrSecurityGroupStateConflict   = errors.New("security group connection state conflict")
	ErrSecurityGroupNameEmpty       = errors.New("security group name is empty")
)

const securityGroupCannotBeApplied = "Network requires port_security_enabled and subnet associated in order to " +
	"apply security groups."

type SecurityGroupError struct {
	Kind    error
	Message string
}

func (e *SecurityGroupError) Error() string { return e.Message }

func (e *SecurityGroupError) Unwrap() error { return e.Kind }

func securityGroupError(kind error, format string, args ...any) error {
	return &SecurityGroupError{Kind: kind, Message: fmt.Sprintf(format, args...)}
}

type ServerSecurityGroup struct {
	ID          string
	Name        string
	Description string
	ProjectID   string
	Rules       []ServerSecurityGroupRule
}

type ServerSecurityGroupRule struct {
	ID            string
	ParentGroupID string
	Protocol      *string
	FromPort      *int
	ToPort        *int
	RemoteGroup   *SecurityGroupReference
	CIDR          string
}

type SecurityGroupReference struct {
	Name      string
	ProjectID string
}

func (s *Servers) AddSecurityGroup(request ActionRequest, serverID, nameOrID string) error {
	projectID := request.ProjectID
	groupID, ports, err := s.securityGroupPorts(projectID, serverID, nameOrID)
	if err != nil {
		return err
	}
	for _, port := range ports {
		if port.PortSecurityDisabled || !port.FixedIP.IsValid() {
			return securityGroupError(ErrSecurityGroupCannotBeApplied, securityGroupCannotBeApplied)
		}
		if slices.Contains(port.SecurityGroups, groupID) {
			return securityGroupError(ErrSecurityGroupCannotBeApplied,
				"Invalid input for operation: Invalid input for security_groups. "+
					"Reason: Duplicate items in the list: '%s'..", groupID)
		}
		groups := append(slices.Clone(port.SecurityGroups), groupID)
		if _, err := s.networks.SetPortSecurityGroups(projectID, port.ID, groups); err != nil {
			if errors.Is(err, networkdomain.ErrSecurityGroupConflict) {
				return securityGroupError(ErrSecurityGroupStateConflict, "%s", err)
			}
			return err
		}
	}
	return nil
}

func (s *Servers) RemoveSecurityGroup(request ActionRequest, serverID, nameOrID string) error {
	projectID := request.ProjectID
	groupID, ports, err := s.securityGroupPorts(projectID, serverID, nameOrID)
	if err != nil {
		return err
	}
	removed := false
	for _, port := range ports {
		index := slices.Index(port.SecurityGroups, groupID)
		if index < 0 {
			continue
		}
		groups := slices.Delete(slices.Clone(port.SecurityGroups), index, index+1)
		if _, err := s.networks.SetPortSecurityGroups(projectID, port.ID, groups); err != nil {
			return err
		}
		removed = true
	}
	if !removed {
		return securityGroupError(ErrSecurityGroupNotFound,
			"Security group %s not associated with the instance %s", nameOrID, serverID)
	}
	return nil
}

func (s *Servers) ListSecurityGroups(projectID, serverID string) ([]ServerSecurityGroup, error) {
	portIDs, err := s.serverPortIDs(projectID, serverID)
	if err != nil {
		return nil, err
	}
	visible := s.networks.ProjectSecurityGroups(projectID)
	references := map[string]SecurityGroupReference{}
	for _, group := range visible {
		references[group.ID] = SecurityGroupReference{Name: group.Name, ProjectID: group.ProjectID}
	}
	groups := []ServerSecurityGroup{}
	for _, portID := range portIDs {
		port, err := s.networks.GetPort(projectID, portID)
		if err != nil || port.DeviceID != serverID {
			continue
		}
		for _, groupID := range port.SecurityGroups {
			index := slices.IndexFunc(visible, func(group networkdomain.SecurityGroup) bool {
				return group.ID == groupID
			})
			if index >= 0 {
				groups = append(groups, novaSecurityGroup(visible[index], references))
			}
		}
	}
	slices.SortStableFunc(groups, func(a, b ServerSecurityGroup) int {
		return cmp.Or(strings.Compare(a.ProjectID, b.ProjectID), strings.Compare(a.Name, b.Name))
	})
	return groups, nil
}

func (s *Servers) securityGroupPorts(
	projectID, serverID, nameOrID string,
) (string, []networkdomain.Port, error) {
	portIDs, err := s.serverPortIDs(projectID, serverID)
	if err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(nameOrID) == "" {
		return "", nil, securityGroupError(ErrSecurityGroupNameEmpty, "Security group name cannot be empty")
	}
	groupID, err := s.findSecurityGroup(projectID, nameOrID)
	if err != nil {
		return "", nil, err
	}
	ports, err := s.deviceSecurityPorts(projectID, serverID, portIDs)
	return groupID, ports, err
}

func (s *Servers) serverPortIDs(projectID, serverID string) ([]string, error) {
	s.compute.mu.RLock()
	defer s.compute.mu.RUnlock()

	record, ok := s.compute.find(projectID, serverID)
	if !ok {
		return nil, ErrServerNotFound
	}
	ids := make([]string, 0, len(record.server.Ports))
	for _, port := range record.server.Ports {
		ids = append(ids, port.ID)
	}
	return ids, nil
}

func (s *Servers) findSecurityGroup(projectID, nameOrID string) (string, error) {
	groups := s.networks.ProjectSecurityGroups(projectID)
	for _, group := range groups {
		if group.ID == nameOrID {
			return group.ID, nil
		}
	}
	var matches []string
	for _, group := range groups {
		if group.Name == nameOrID {
			matches = append(matches, group.ID)
		}
	}
	switch len(matches) {
	case 0:
		return "", securityGroupError(ErrSecurityGroupNotFound,
			"Security group %s is not found for project %s", nameOrID, projectID)
	case 1:
		return matches[0], nil
	default:
		return "", securityGroupError(ErrSecurityGroupAmbiguous,
			"Multiple security_group matches found for name '%s', use an ID to be more specific.", nameOrID)
	}
}

func (s *Servers) deviceSecurityPorts(projectID, serverID string, portIDs []string) ([]networkdomain.Port, error) {
	var ports []networkdomain.Port
	for _, portID := range portIDs {
		port, err := s.networks.GetPort(projectID, portID)
		if err == nil && port.DeviceID == serverID {
			ports = append(ports, port)
		}
	}
	if len(ports) == 0 {
		return nil, securityGroupError(ErrSecurityGroupNotFound,
			"instance_id %s could not be found as device id on any ports", serverID)
	}
	return ports, nil
}

func novaSecurityGroup(
	group networkdomain.SecurityGroup, references map[string]SecurityGroupReference,
) ServerSecurityGroup {
	converted := ServerSecurityGroup{
		ID:          group.ID,
		Name:        group.Name,
		Description: group.Description,
		ProjectID:   group.ProjectID,
		Rules:       []ServerSecurityGroupRule{},
	}
	for _, rule := range group.Rules {
		if rule.Direction != "ingress" {
			continue
		}
		novaRule := ServerSecurityGroupRule{
			ID:            rule.ID,
			ParentGroupID: group.ID,
			FromPort:      copyPort(rule.PortRangeMin),
			ToPort:        copyPort(rule.PortRangeMax),
		}
		if rule.Protocol != "" {
			protocol := rule.Protocol
			novaRule.Protocol = &protocol
			if rule.PortRangeMin == nil && rule.PortRangeMax == nil {
				from, to := -1, -1
				if upper := strings.ToUpper(protocol); upper == "TCP" || upper == "UDP" {
					from, to = 1, 65535
				}
				novaRule.FromPort, novaRule.ToPort = &from, &to
			}
		}
		if rule.RemoteGroupID != "" {
			reference, ok := references[rule.RemoteGroupID]
			if !ok {
				continue
			}
			novaRule.RemoteGroup = &reference
		} else if rule.RemoteIPPrefix.IsValid() {
			novaRule.CIDR = rule.RemoteIPPrefix.String()
		} else {
			novaRule.CIDR = "0.0.0.0/0"
		}
		converted.Rules = append(converted.Rules, novaRule)
	}
	return converted
}

func copyPort(port *int) *int {
	if port == nil {
		return nil
	}
	value := *port
	return &value
}
