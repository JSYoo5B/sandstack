package volume

import domain "github.com/JSYoo5B/sandstack/internal/domain/volume"

// StandardAvailabilityZone is the zone used by the fixture's volumes.
const StandardAvailabilityZone = "nova"

// ListAvailabilityZones returns the zone shared by every project.
func (s *Service) ListAvailabilityZones() []domain.AvailabilityZone {
	return []domain.AvailabilityZone{{Name: StandardAvailabilityZone, Available: true}}
}
