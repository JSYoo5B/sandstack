package compute

import (
	"errors"

	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

const FixtureFlavorID = "1"

var ErrFlavorNotFound = errors.New("compute flavor not found")

// GetFlavor returns the startup server's immutable flavor.
func (s *Service) GetFlavor(projectID, flavorID string) (domain.Flavor, error) {
	if projectID != s.seed.ProjectID || flavorID != FixtureFlavorID {
		return domain.Flavor{}, ErrFlavorNotFound
	}
	return domain.Flavor{
		ID:         FixtureFlavorID,
		Name:       "m1.tiny",
		RAMMiB:     512,
		DiskGiB:    1,
		VCPUs:      1,
		Public:     true,
		RxTxFactor: 1,
	}, nil
}
