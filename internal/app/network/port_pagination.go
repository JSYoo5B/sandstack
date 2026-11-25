package network

import (
	"slices"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

type PortPageOptions struct {
	Marker  string
	Limit   int
	Order   PortListOrder
	Reverse bool
}

type PortPage struct {
	Ports       []domain.Port
	HasNext     bool
	HasPrevious bool
}

// ListPorts returns the project's matching ports ordered by ID.
func (s *Service) ListPorts(projectID string, filter PortListFilter) []domain.Port {
	page, _ := s.ListPortsPage(projectID, filter, PortPageOptions{})
	return page.Ports
}

// ListPortsPage keeps the requested sort order in both directions.
func (s *Service) ListPortsPage(
	projectID string,
	filter PortListFilter,
	options PortPageOptions,
) (PortPage, error) {
	ports := []domain.Port{}
	var marker domain.Port
	s.mu.RLock()
	if options.Limit > 0 && options.Marker != "" {
		var exists bool
		marker, exists = s.ports[options.Marker]
		if !exists || marker.ProjectID != projectID {
			s.mu.RUnlock()
			return PortPage{}, ErrPortNotFound
		}
	}
	for _, port := range s.ports {
		if port.ProjectID == projectID && filter.matches(port) {
			ports = append(ports, port)
		}
	}
	s.mu.RUnlock()
	slices.SortFunc(ports, options.Order.compare)

	if options.Limit <= 0 {
		return PortPage{Ports: ports}, nil
	}
	start, end := 0, len(ports)
	if marker.ID != "" {
		index, found := slices.BinarySearchFunc(ports, marker, options.Order.compare)
		if options.Reverse {
			end = index
		} else {
			start = index
			if found {
				start++
			}
		}
	}
	if options.Reverse {
		start = max(0, end-options.Limit)
	} else {
		end = start + min(options.Limit, end-start)
	}
	return PortPage{
		Ports:       ports[start:end],
		HasNext:     start < end && end < len(ports),
		HasPrevious: start < end && start > 0,
	}, nil
}
