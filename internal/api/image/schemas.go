package image

import "net/http"

const (
	imageSchemaPath     = Path + "/schemas/image"
	imageListSchemaPath = Path + "/schemas/images"
)

type schemaType struct {
	Type      any    `json:"type"`
	Pattern   string `json:"pattern,omitempty"`
	Enum      []any  `json:"enum,omitempty"`
	ReadOnly  bool   `json:"readOnly,omitempty"`
	MaxLength int    `json:"maxLength,omitempty"`
	Items     any    `json:"items,omitempty"`
}

type schemaBody struct {
	Name                 string                `json:"name"`
	Properties           map[string]schemaType `json:"properties"`
	AdditionalProperties *schemaType           `json:"additionalProperties,omitempty"`
	Links                []schemaLink          `json:"links,omitempty"`
}

type schemaLink struct {
	Rel  string `json:"rel"`
	Href string `json:"href"`
}

var imageSchema = schemaBody{
	Name: "image",
	Properties: map[string]schemaType{
		"id": {
			Type:    "string",
			Pattern: "^[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$",
		},
		"name":             {Type: []string{"null", "string"}, MaxLength: maxImageTextLength},
		"owner":            {Type: []string{"null", "string"}, MaxLength: 255},
		"status":           {Type: "string", ReadOnly: true, Enum: []any{"queued", "active", "deactivated"}},
		"visibility":       {Type: "string", Enum: []any{fixtureVisibility}},
		"created_at":       {Type: "string", ReadOnly: true},
		"updated_at":       {Type: "string", ReadOnly: true},
		"container_format": {Type: []string{"null", "string"}, Enum: []any{nil, fixtureContainerFormat}},
		"disk_format":      {Type: []string{"null", "string"}, Enum: []any{nil, fixtureDiskFormat}},
		"min_disk":         {Type: "integer"},
		"min_ram":          {Type: "integer"},
		"protected":        {Type: "boolean"},
		"tags":             {Type: "array", Items: schemaType{Type: "string", MaxLength: maxImageTextLength}},
		"checksum":         {Type: []string{"null", "string"}, MaxLength: 32, ReadOnly: true},
		"size":             {Type: []string{"null", "integer"}, ReadOnly: true},
		"virtual_size":     {Type: []string{"null", "integer"}, ReadOnly: true},
		"self":             {Type: "string", ReadOnly: true},
		"file":             {Type: "string", ReadOnly: true},
		"schema":           {Type: "string", ReadOnly: true},
	},
	AdditionalProperties: &schemaType{Type: "string"},
	Links: []schemaLink{
		{Rel: "self", Href: "{self}"},
		{Rel: "enclosure", Href: "{file}"},
		{Rel: "describedby", Href: "{schema}"},
	},
}

var imageListSchema = schemaBody{
	Name: "images",
	Properties: map[string]schemaType{
		"images": {Type: "array", Items: imageSchema},
		"first":  {Type: "string"},
		"next":   {Type: "string"},
		"schema": {Type: "string"},
	},
	Links: []schemaLink{
		{Rel: "first", Href: "{first}"},
		{Rel: "next", Href: "{next}"},
		{Rel: "describedby", Href: "{schema}"},
	},
}

var memberSchema = schemaBody{
	Name: "member",
	Properties: map[string]schemaType{
		"member_id": {Type: "string"},
		"image_id": {
			Type:    "string",
			Pattern: "^([0-9a-fA-F]){8}-([0-9a-fA-F]){4}-([0-9a-fA-F]){4}-([0-9a-fA-F]){4}-([0-9a-fA-F]){12}$",
		},
		"created_at": {Type: "string"},
		"updated_at": {Type: "string"},
		"status":     {Type: "string", Enum: []any{"pending", "accepted", "rejected"}},
		"schema":     {Type: "string", ReadOnly: true},
	},
}

var memberListSchema = schemaBody{
	Name: "members",
	Properties: map[string]schemaType{
		"members": {Type: "array", Items: memberSchema},
		"schema":  {Type: "string"},
	},
	Links: []schemaLink{{Rel: "describedby", Href: "{schema}"}},
}

func showSchema(schema schemaBody) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, _ *http.Request, _ string) {
		writeJSON(w, http.StatusOK, schema)
	}
}
