package volume

import (
	"net/http"
	"strconv"

	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

func (h handler) describeDetailedVolume(r *http.Request, volume domain.Volume) volumeBody {
	body := h.describeVolume(r, volume)
	source := volume.ImageSource
	if !volume.ImageMetadataReady || source.ID == "" {
		return body
	}
	body.ImageMetadata = map[string]string{
		"image_id": source.ID,
		"size":     strconv.FormatInt(source.Size, 10),
		"min_disk": strconv.Itoa(source.MinDiskGiB),
		"min_ram":  strconv.Itoa(source.MinRAMMiB),
	}
	for key, value := range map[string]string{
		"image_name":       source.Name,
		"container_format": source.ContainerFormat,
		"disk_format":      source.DiskFormat,
		"checksum":         source.Checksum,
	} {
		if value != "" {
			body.ImageMetadata[key] = value
		}
	}
	return body
}
