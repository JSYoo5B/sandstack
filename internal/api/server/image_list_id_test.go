package server_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudFiltersGlanceImageListByID(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client, err := openstack.NewImageV2(provider, gophercloud.EndpointOpts{
		Region:       "RegionOne",
		Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	create := func(name string) string {
		t.Helper()
		private := images.ImageVisibilityPrivate
		created, err := images.Create(ctx, client, images.CreateOpts{
			Name: name, Visibility: &private, ContainerFormat: "bare", DiskFormat: "qcow2",
		}).Extract()
		require.NoError(t, err)
		require.NotEmpty(t, created.ID)
		require.Equal(t, images.ImageStatusQueued, created.Status)
		return created.ID
	}
	first := create("first queued")
	second := create("second queued")
	require.NotEqual(t, first, second)

	for _, tt := range []struct {
		name string
		opts images.ListOpts
		want []string
	}{
		{name: "bare", opts: images.ListOpts{ID: first}, want: []string{first}},
		{name: "explicit equality", opts: images.ListOpts{ID: "eq:" + second}, want: []string{second}},
		{
			name: "mixed quoted and bare IDs",
			opts: images.ListOpts{ID: `in:"` + first + `",` + fixtureImageID},
			want: []string{first, fixtureImageID},
		},
		{
			name: "status conjunction across pages",
			opts: images.ListOpts{
				ID:     `in:` + fixtureImageID + `,"` + first + `",` + second,
				Status: images.ImageStatusQueued, Limit: 1,
			},
			want: []string{first, second},
		},
		{
			name: "matching ID with different status",
			opts: images.ListOpts{ID: fixtureImageID, Status: images.ImageStatusQueued},
		},
		{name: "unknown ID", opts: images.ListOpts{ID: "00000000-0000-0000-0000-000000000000"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pages, err := images.List(client, tt.opts).AllPages(t.Context())
			require.NoError(t, err)
			rows, err := images.ExtractImages(pages)
			require.NoError(t, err)
			ids := make([]string, 0, len(rows))
			for _, row := range rows {
				ids = append(ids, row.ID)
			}
			assert.ElementsMatch(t, tt.want, ids)
		})
	}
}

func TestGlanceImageListIDFilterWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	const missingID = "00000000-0000-0000-0000-000000000000"

	// https://docs.openstack.org/api-ref/image/v2/#list-images
	for _, tt := range []struct {
		name, id string
		match    bool
	}{
		{name: "empty bare", id: ""},
		{name: "empty equality", id: "eq:"},
		{name: "empty membership", id: "in:"},
		{name: "space after comma is literal", id: "in:" + missingID + ", " + fixtureImageID},
		{name: "exact after comma", id: "in:" + missingID + "," + fixtureImageID, match: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			query := url.Values{"id": {tt.id}, "limit": {"1"}}
			status, headers, body := request(t, instance, http.MethodGet, imagesPath+"?"+query.Encode(), "", auth)

			require.Equal(t, http.StatusOK, status, "%s", body)
			requireJSON(t, headers)
			var page map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(body, &page))
			if !tt.match {
				assert.JSONEq(t, "[]", string(page["images"]))
				assert.NotContains(t, page, "next")
				return
			}
			var rows []struct {
				ID string `json:"id"`
			}
			require.NoError(t, json.Unmarshal(page["images"], &rows))
			require.Len(t, rows, 1)
			assert.Equal(t, fixtureImageID, rows[0].ID)
			var next string
			require.NoError(t, json.Unmarshal(page["next"], &next))
			href, err := url.Parse(next)
			require.NoError(t, err)
			assert.Equal(t, "/v2/images", href.Path)
			query.Set("marker", fixtureImageID)
			assert.Equal(t, query, href.Query())
		})
	}

	for _, tt := range []struct {
		name string
		ids  []string
	}{
		{name: "unclosed quote", ids: []string{`in:"` + fixtureImageID}},
		{name: "unsupported operator", ids: []string{"neq:" + fixtureImageID}},
		{name: "repeated id", ids: []string{fixtureImageID, missingID}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			query := url.Values{"id": tt.ids}
			status, _, body := request(t, instance, http.MethodGet, imagesPath+"?"+query.Encode(), "", auth)
			assert.Equal(t, http.StatusBadRequest, status, "%s", body)
		})
	}
}
