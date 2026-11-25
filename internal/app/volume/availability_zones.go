package volume

import domain "github.com/JSYoo5B/sandstack/internal/domain/volume"

// StandardAvailabilityZone is the zone used by the fixture's volumes.
const StandardAvailabilityZone = "nova"

// ListAvailabilityZones returns the available zone for the fixture project.
func (s *Service) ListAvailabilityZones(projectID string) []domain.AvailabilityZone {
	if s.seed.ProjectID != projectID {
		return []domain.AvailabilityZone{}
	}
	return []domain.AvailabilityZone{{Name: StandardAvailabilityZone, Available: true}}
}
