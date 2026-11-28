package volume

import (
	"cmp"
	"crypto/subtle"
	"errors"
	"slices"

	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

var (
	ErrTransferNotFound      = errors.New("transfer not found")
	ErrTransferVolumeState   = errors.New("status must be available")
	ErrTransferAuthKey       = errors.New("invalid auth key")
	ErrTransferKeyGeneration = errors.New("transfer auth key generation failed")
)

type TransferSnapshotStateError struct {
	SnapshotID string
}

func (e TransferSnapshotStateError) Error() string {
	return "snapshot: " + e.SnapshotID + " status must be available"
}

type TransferAcceptSnapshotStateError struct {
	TransferID string
	SnapshotID string
}

func (e TransferAcceptSnapshotStateError) Error() string {
	return "Transfer " + e.TransferID + ": Snapshot " + e.SnapshotID + " is not in the expected available state."
}

type CreatedTransfer struct {
	Transfer domain.Transfer
	AuthKey  string
}

func (s *Service) WithTransferKeys(newAuthKey func() string) *Service {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.newAuthKey = newAuthKey
	return s
}

func (s *Service) CreateTransfer(projectID, volumeID string, name *string) (CreatedTransfer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	volume, ok := s.ownedVolume(projectID, volumeID)
	if !ok {
		return CreatedTransfer{}, ErrVolumeNotFound
	}
	if volume.State != domain.StateAvailable {
		return CreatedTransfer{}, ErrTransferVolumeState
	}
	if snapshotID := s.unavailableSnapshot(volumeID); snapshotID != "" {
		return CreatedTransfer{}, TransferSnapshotStateError{SnapshotID: snapshotID}
	}
	var authKey string
	if s.newAuthKey != nil {
		authKey = s.newAuthKey()
	}
	if authKey == "" {
		return CreatedTransfer{}, ErrTransferKeyGeneration
	}
	id, err := s.allocateID()
	if err != nil {
		return CreatedTransfer{}, err
	}
	transfer := domain.Transfer{
		ID:        id,
		ProjectID: projectID,
		VolumeID:  volumeID,
		Name:      optionalText(name),
		AuthKey:   authKey,
		CreatedAt: s.now().UTC(),
	}
	s.transfers[id] = transfer
	s.updateState(volume, domain.StateAwaitingTransfer)
	return CreatedTransfer{Transfer: withoutAuthKey(transfer), AuthKey: authKey}, nil
}

func (s *Service) unavailableSnapshot(volumeID string) string {
	var pending []string
	for _, snapshot := range s.snapshots {
		if snapshot.VolumeID == volumeID && snapshot.State != domain.StateAvailable {
			pending = append(pending, snapshot.ID)
		}
	}
	if len(pending) == 0 {
		return ""
	}
	return slices.Min(pending)
}

func (s *Service) GetTransfer(projectID, transferID string) (domain.Transfer, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	transfer, ok := s.transfers[transferID]
	if !ok || transfer.ProjectID != projectID {
		return domain.Transfer{}, ErrTransferNotFound
	}
	return withoutAuthKey(transfer), nil
}

func (s *Service) ListTransfers(projectID string) []domain.Transfer {
	s.mu.RLock()
	defer s.mu.RUnlock()

	transfers := []domain.Transfer{}
	for _, transfer := range s.transfers {
		if transfer.ProjectID == projectID {
			transfers = append(transfers, withoutAuthKey(transfer))
		}
	}
	slices.SortFunc(transfers, func(a, b domain.Transfer) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	})
	return transfers
}

func (s *Service) DeleteTransfer(projectID, transferID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	transfer, ok := s.transfers[transferID]
	if !ok || transfer.ProjectID != projectID {
		return ErrTransferNotFound
	}
	delete(s.transfers, transferID)
	if volume, found := s.volumes[transfer.VolumeID]; found && volume.State == domain.StateAwaitingTransfer {
		s.updateState(volume, domain.StateAvailable)
	}
	return nil
}

func (s *Service) AcceptTransfer(projectID, userID, transferID, authKey string) (domain.Transfer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	transfer, ok := s.transfers[transferID]
	if !ok {
		return domain.Transfer{}, ErrTransferNotFound
	}
	if subtle.ConstantTimeCompare([]byte(authKey), []byte(transfer.AuthKey)) != 1 {
		return domain.Transfer{}, ErrTransferAuthKey
	}
	volume, found := s.volumes[transfer.VolumeID]
	if !found || volume.State != domain.StateAwaitingTransfer {
		return domain.Transfer{}, ErrVolumeStateConflict
	}
	if snapshotID := s.unavailableSnapshot(volume.ID); snapshotID != "" {
		s.updateState(volume, domain.StateAwaitingTransfer)
		return domain.Transfer{}, TransferAcceptSnapshotStateError{TransferID: transferID, SnapshotID: snapshotID}
	}
	now := s.now().UTC()
	for id, snapshot := range s.snapshots {
		if snapshot.VolumeID == volume.ID {
			snapshot.ProjectID = projectID
			snapshot.UpdatedAt = now
			s.snapshots[id] = snapshot
		}
	}
	volume.ProjectID = projectID
	volume.UserID = userID
	s.updateState(volume, domain.StateAvailable)
	delete(s.transfers, transferID)
	return withoutAuthKey(transfer), nil
}

func withoutAuthKey(transfer domain.Transfer) domain.Transfer {
	transfer.AuthKey = ""
	return transfer
}
