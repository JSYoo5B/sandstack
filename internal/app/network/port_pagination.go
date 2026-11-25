package network

import domain "github.com/JSYoo5B/sandstack/internal/domain/network"

type PortPageOptions struct {
	PageOptions
	Order PortListOrder
}

// ListPorts returns the project's matching ports ordered by ID.
func (s *Service) ListPorts(projectID string, filter PortListFilter) []domain.Port {
	page, _ := s.ListPortsPage(projectID, filter, PortPageOptions{})
	return page.Items
}

// ListPortsPage keeps the requested sort order in both directions.
func (s *Service) ListPortsPage(
	projectID string,
	filter PortListFilter,
	options PortPageOptions,
) (Page[domain.Port], error) {
	ports := []domain.Port{}
	var marker *domain.Port
	s.mu.RLock()
	if options.Limit > 0 && options.Marker != "" {
		port, exists := s.ports[options.Marker]
		if !exists || port.ProjectID != projectID {
			s.mu.RUnlock()
			return Page[domain.Port]{}, ErrPortNotFound
		}
		marker = &port
	}
	for _, port := range s.ports {
		if port.ProjectID == projectID && filter.matches(port) {
			ports = append(ports, clonePort(port))
		}
	}
	s.mu.RUnlock()
	return paginate(ports, options.PageOptions, marker, options.Order.compare, func(port domain.Port) string {
		return port.ID
	}), nil
}
