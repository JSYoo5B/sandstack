package volume

import (
	"errors"

	imagedomain "github.com/JSYoo5B/sandstack/internal/domain/image"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

var (
	ErrUploadImageUnavailable = errors.New("the image service is not available for volume uploads")
	ErrUploadImageRejected    = errors.New("the image service rejected the volume image")
)

type imageUploads interface {
	CreateVolumeImage(projectID string, template imagedomain.Image) (imagedomain.Image, error)
	UploadImageData(projectID, imageID, sourceImageID string) error
	Delete(projectID, imageID string) error
}

type UploadImageRequest struct {
	ImageName       string
	ContainerFormat string
	DiskFormat      string
	Force           bool
}

type UploadedImage struct {
	Volume     domain.Volume
	VolumeType domain.VolumeType
	Image      imagedomain.Image
}

func (s *Service) WithImageUploads(uploads imageUploads) *Service {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.uploads = uploads
	return s
}

func (s *Service) UploadImage(projectID, volumeID string, request UploadImageRequest) (UploadedImage, error) {
	if s.seed.ProjectID != projectID {
		return UploadedImage{}, ErrVolumeNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	volume, ok := s.volumes[volumeID]
	if !ok {
		return UploadedImage{}, ErrVolumeNotFound
	}
	if volume.State != domain.StateAvailable {
		return UploadedImage{}, ErrVolumeStateConflict
	}
	if s.uploads == nil {
		return UploadedImage{}, ErrUploadImageUnavailable
	}
	image, err := s.uploads.CreateVolumeImage(projectID, imagedomain.Image{
		Name:            request.ImageName,
		ContainerFormat: request.ContainerFormat,
		DiskFormat:      request.DiskFormat,
	})
	if err != nil {
		return UploadedImage{}, errors.Join(ErrUploadImageRejected, err)
	}
	volumeType, _ := findVolumeType(effectiveVolumeTypeID(volume.TypeID))
	uploaded := UploadedImage{Volume: cloneVolume(volume), VolumeType: volumeType, Image: image}

	var sourceImageID string
	if volume.ImageMetadataReady {
		sourceImageID = volume.ImageSource.ID
	}
	if err := s.uploads.UploadImageData(projectID, image.ID, sourceImageID); err != nil {
		_ = s.uploads.Delete(projectID, image.ID)
	}
	s.updateState(volume, domain.StateAvailable)
	return uploaded, nil
}
