package compute

import (
	"encoding/json"
	"regexp"
	"slices"
	"strconv"

	appcompute "github.com/JSYoo5B/sandstack/internal/app/compute"
)

const (
	blockDeviceField = "block_device_mapping_v2"
	maxVolumeSize    = 0x7FFFFFFF
)

var (
	blockDeviceSchemaFields = []string{
		"virtual_name", "volume_id", "snapshot_id", "volume_size", "device_name", "delete_on_termination",
		"no_device", "connection_info", "source_type", "uuid", "image_id", "destination_type", "guest_format",
		"device_type", "disk_bus", "boot_index",
	}
	supportedBlockDeviceFields = []string{
		"source_type", "uuid", "destination_type", "boot_index", "delete_on_termination", "volume_size",
	}
	blockDeviceUUIDPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]*$`)
	signedIntegerPattern   = regexp.MustCompile(`^-?[0-9]+$`)
	unsignedIntegerPattern = regexp.MustCompile(`^[0-9]+$`)
	booleanValues          = map[string]bool{
		"True": true, "TRUE": true, "true": true, "1": true, "ON": true, "On": true, "on": true,
		"YES": true, "Yes": true, "yes": true,
		"False": false, "FALSE": false, "false": false, "0": false, "OFF": false, "Off": false, "off": false,
		"NO": false, "No": false, "no": false,
	}
)

type blockDeviceItem struct {
	fields          map[string]json.RawMessage
	sourceType      *string
	uuid            *string
	destinationType *string
	device          appcompute.BlockDevice
}

func parseBlockDeviceItems(raw json.RawMessage) ([]blockDeviceItem, error) {
	if raw == nil {
		return nil, nil
	}
	invalid := badCreateRequest("Invalid input for field '" + blockDeviceField + "'.")
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) != nil || values == nil {
		return nil, invalid
	}
	items := make([]blockDeviceItem, 0, len(values))
	for _, value := range values {
		fields, err := readUniqueObject(value)
		if err != nil {
			return nil, invalid
		}
		item := blockDeviceItem{fields: fields}
		for key, field := range fields {
			if !slices.Contains(blockDeviceSchemaFields, key) || !validBlockDeviceField(key, field, &item) {
				return nil, invalid
			}
		}
		items = append(items, item)
	}
	return items, nil
}

func validBlockDeviceField(key string, raw json.RawMessage, item *blockDeviceItem) bool {
	switch key {
	case "source_type":
		var value string
		ok := json.Unmarshal(raw, &value) == nil &&
			slices.Contains([]string{"volume", "image", "snapshot", "blank"}, value)
		item.sourceType = &value
		return ok
	case "destination_type":
		var value string
		ok := json.Unmarshal(raw, &value) == nil && (value == "local" || value == "volume")
		item.destinationType = &value
		return ok
	case "uuid":
		var value string
		ok := json.Unmarshal(raw, &value) == nil && value != "" && len(value) <= 255 &&
			blockDeviceUUIDPattern.MatchString(value)
		item.uuid = &value
		return ok
	case "boot_index":
		return parseBootIndex(raw, item)
	case "delete_on_termination":
		var flag bool
		if json.Unmarshal(raw, &flag) == nil {
			item.device.DeleteOnTermination = flag
			return true
		}
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return false
		}
		flag, known := booleanValues[text]
		item.device.DeleteOnTermination = flag
		return known
	case "volume_size":
		size, ok := parseSchemaInteger(raw, unsignedIntegerPattern)
		item.device.VolumeSize = int(size)
		return ok && size >= 1 && size <= maxVolumeSize
	default:
		return true
	}
}

func parseBootIndex(raw json.RawMessage, item *blockDeviceItem) bool {
	if string(raw) == "null" {
		return true
	}
	index, ok := parseSchemaInteger(raw, signedIntegerPattern)
	if ok {
		value := int(index)
		item.device.BootIndex = &value
	}
	return ok
}

func parseSchemaInteger(raw json.RawMessage, pattern *regexp.Regexp) (int64, bool) {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		if !pattern.MatchString(text) {
			return 0, false
		}
	} else {
		var number json.Number
		if json.Unmarshal(raw, &number) != nil {
			return 0, false
		}
		text = number.String()
	}
	value, err := strconv.ParseInt(text, 10, 64)
	return value, err == nil
}

func requestedBlockDevices(items []blockDeviceItem, imageRef string) ([]appcompute.BlockDevice, error) {
	if len(items) == 0 {
		return nil, nil
	}
	devices := make([]appcompute.BlockDevice, 0, len(items))
	for _, item := range items {
		sourceType := ""
		if item.sourceType != nil {
			sourceType = *item.sourceType
		}
		uuid := ""
		if item.uuid != nil {
			uuid = *item.uuid
		}
		switch {
		case sourceType == "blank" && uuid != "":
			return nil, badCreateRequest("Block Device Mapping is Invalid: Invalid device UUID.")
		case sourceType != "blank" && uuid == "":
			return nil, badCreateRequest("Block Device Mapping is Invalid: Missing device UUID.")
		}
		destinationType := ""
		if item.destinationType != nil {
			destinationType = *item.destinationType
		}
		if sourceType == "image" && destinationType == "local" &&
			(imageRef == "" || item.device.BootIndex == nil || *item.device.BootIndex != 0) {
			return nil, badCreateRequest("Block Device Mapping is Invalid: Mapping image to local is not supported.")
		}
		if item.sourceType == nil {
			return nil, badCreateRequest("Block Device Mapping is Invalid: Following fields are invalid: None_id")
		}
		device := item.device
		device.SourceType = sourceType
		device.UUID = uuid
		devices = append(devices, device)
	}

	unsupported := badCreateRequest("SandStack supports one block device from a volume or an image to a volume " +
		"without imageRef.")
	if len(items) > 1 || imageRef != "" {
		return nil, unsupported
	}
	for key := range items[0].fields {
		if !slices.Contains(supportedBlockDeviceFields, key) {
			return nil, badCreateRequest("The block device field '" + key + "' is not supported.")
		}
	}
	if items[0].destinationType == nil || *items[0].destinationType != "volume" ||
		(devices[0].SourceType != appcompute.BlockDeviceSourceVolume &&
			devices[0].SourceType != appcompute.BlockDeviceSourceImage) {
		return nil, unsupported
	}
	return devices, nil
}
