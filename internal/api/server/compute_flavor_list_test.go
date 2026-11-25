package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/flavors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudListsNovaFlavorDetailsByMinimumResources(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	compute, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Region:       "RegionOne",
		Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	detail, err := flavors.Get(t.Context(), compute, "1").Extract()
	require.NoError(t, err)
	require.Equal(t, "1", detail.ID)

	for _, tt := range []struct {
		name string
		opts flavors.ListOpts
		want []flavors.Flavor
	}{
		{name: "unfiltered", want: []flavors.Flavor{*detail}},
		{
			name: "both inclusive boundaries",
			opts: flavors.ListOpts{MinRAM: 512, MinDisk: 1},
			want: []flavors.Flavor{*detail},
		},
		{name: "too little RAM alone", opts: flavors.ListOpts{MinRAM: 513}},
		{name: "too little disk alone", opts: flavors.ListOpts{MinDisk: 2}},
		{name: "meeting only RAM", opts: flavors.ListOpts{MinRAM: 512, MinDisk: 2}},
		{name: "meeting only disk", opts: flavors.ListOpts{MinRAM: 513, MinDisk: 1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			page, err := flavors.ListDetail(compute, tt.opts).AllPages(t.Context())
			require.NoError(t, err)
			rows, err := flavors.ExtractFlavors(page)
			require.NoError(t, err)

			assert.ElementsMatch(t, tt.want, rows)
		})
	}
}

func TestNovaFlavorListDetailWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	project := routerProjectID(t, provider)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	path := computePath + "/" + project + "/flavors"
	status, _, body := request(t, instance, http.MethodGet, path+"/1", "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	var detail struct {
		Flavor json.RawMessage `json:"flavor"`
	}
	require.NoError(t, json.Unmarshal(body, &detail))
	require.NotEmpty(t, detail.Flavor)

	// https://docs.openstack.org/api-ref/compute/#list-flavors-with-details
	// https://github.com/openstack/nova/blob/master/nova/api/openstack/compute/flavors.py#L190-L223
	for _, tt := range []struct {
		name  string
		query string
		match bool
	}{
		{name: "exact boundaries", query: "?minRam=512&minDisk=1", match: true},
		{name: "signed minima", query: "?minRam=%2B512&minDisk=-1", match: true},
		{name: "zero minima", query: "?minRam=0&minDisk=0", match: true},
		{name: "disk minimum excludes", query: "?minRam=512&minDisk=2"},
		{name: "RAM minimum excludes", query: "?minRam=513&minDisk=1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodGet, path+"/detail"+tt.query, "", auth)

			require.Equal(t, http.StatusOK, status, "%s", body)
			requireJSON(t, headers)
			assert.Equal(t, "compute 2.1", headers.Get("OpenStack-API-Version"))
			expected := `{"flavors":[]}`
			if tt.match {
				expected = `{"flavors":[` + string(detail.Flavor) + `]}`
			}
			assert.JSONEq(t, expected, string(body))
		})
	}

	for _, tt := range []struct {
		name  string
		query string
	}{
		{name: "noninteger RAM", query: "?minRam=512.5"},
		{name: "noninteger disk", query: "?minDisk=GiB"},
		{name: "empty RAM", query: "?minRam="},
		{name: "empty disk", query: "?minDisk"},
		{name: "repeated RAM", query: "?minRam=512&minRam=512"},
		{name: "repeated disk", query: "?minDisk=1&minDisk=2"},
		{name: "unsupported", query: "?limit=1"},
		{name: "malformed URL escaping", query: "?minDisk=%zz"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodGet, path+"/detail"+tt.query, "", auth)

			require.Equal(t, http.StatusBadRequest, status, "%s", body)
			requireJSON(t, headers)
			var response struct {
				BadRequest struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				} `json:"badRequest"`
			}
			require.NoError(t, json.Unmarshal(body, &response))
			assert.Equal(t, http.StatusBadRequest, response.BadRequest.Code)
			assert.NotEmpty(t, response.BadRequest.Message)
		})
	}
}
