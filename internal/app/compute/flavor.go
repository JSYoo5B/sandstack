package compute

import (
	"errors"

	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

const (
	FixtureFlavorID      = "1"
	FixtureSmallFlavorID = "2"
)

var ErrFlavorNotFound = errors.New("compute flavor not found")

// GetFlavor returns one of the immutable fixture flavors.
func (s *Service) GetFlavor(projectID, flavorID string) (domain.Flavor, error) {
	if projectID != s.seed.ProjectID {
		return domain.Flavor{}, ErrFlavorNotFound
	}
	for _, flavor := range fixtureFlavors() {
		if flavor.ID == flavorID {
			return flavor, nil
		}
	}
	return domain.Flavor{}, ErrFlavorNotFound
}

// ListFlavors returns the fixture flavors that meet both minima in flavor ID
// order, as Nova's default flavor sort does.
func (s *Service) ListFlavors(projectID string, minRAM, minDisk int) []domain.Flavor {
	flavors := []domain.Flavor{}
	if projectID != s.seed.ProjectID {
		return flavors
	}
	for _, flavor := range fixtureFlavors() {
		if flavor.RAMMiB >= minRAM && flavor.DiskGiB >= minDisk {
			flavors = append(flavors, flavor)
		}
	}
	return flavors
}

func fixtureFlavors() []domain.Flavor {
	return []domain.Flavor{
		{
			ID:         FixtureFlavorID,
			Name:       "m1.tiny",
			RAMMiB:     512,
			DiskGiB:    1,
			VCPUs:      1,
			Public:     true,
			RxTxFactor: 1,
		},
		{
			ID:         FixtureSmallFlavorID,
			Name:       "m1.small",
			RAMMiB:     2048,
			DiskGiB:    20,
			VCPUs:      1,
			Public:     true,
			RxTxFactor: 1,
		},
	}
}
