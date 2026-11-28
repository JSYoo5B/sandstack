package server_test

import (
	"encoding/json"
	"net/http"
	"strings"
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
	tiny, err := flavors.Get(t.Context(), compute, "1").Extract()
	require.NoError(t, err)
	require.Equal(t, "m1.tiny", tiny.Name)
	small, err := flavors.Get(t.Context(), compute, "2").Extract()
	require.NoError(t, err)
	require.Equal(t, "m1.small", small.Name)

	for _, tt := range []struct {
		name string
		opts flavors.ListOpts
		want []flavors.Flavor
	}{
		{name: "unfiltered", want: []flavors.Flavor{*tiny, *small}},
		{
			name: "tiny inclusive boundaries",
			opts: flavors.ListOpts{MinRAM: 512, MinDisk: 1},
			want: []flavors.Flavor{*tiny, *small},
		},
		{name: "RAM above tiny", opts: flavors.ListOpts{MinRAM: 513}, want: []flavors.Flavor{*small}},
		{name: "disk above tiny", opts: flavors.ListOpts{MinDisk: 2}, want: []flavors.Flavor{*small}},
		{
			name: "small inclusive boundaries",
			opts: flavors.ListOpts{MinRAM: 2048, MinDisk: 20},
			want: []flavors.Flavor{*small},
		},
		{name: "RAM above every flavor", opts: flavors.ListOpts{MinRAM: 2049}},
		{name: "disk above every flavor", opts: flavors.ListOpts{MinDisk: 21}},
		{name: "meeting only small RAM", opts: flavors.ListOpts{MinRAM: 2048, MinDisk: 21}},
		{name: "meeting only small disk", opts: flavors.ListOpts{MinRAM: 2049, MinDisk: 20}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			page, err := flavors.ListDetail(compute, tt.opts).AllPages(t.Context())
			require.NoError(t, err)
			rows, err := flavors.ExtractFlavors(page)
			require.NoError(t, err)

			assert.Equal(t, tt.want, rows)
		})
	}
}

func TestNovaFlavorListDetailWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	project := routerProjectID(t, provider)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	path := computePath + "/" + project + "/flavors"
	details := make([]string, 2)
	for i, id := range []string{"1", "2"} {
		status, _, body := request(t, instance, http.MethodGet, path+"/"+id, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		var detail struct {
			Flavor json.RawMessage `json:"flavor"`
		}
		require.NoError(t, json.Unmarshal(body, &detail))
		require.NotEmpty(t, detail.Flavor)
		details[i] = string(detail.Flavor)
	}
	tiny, small := details[0], details[1]

	// https://docs.openstack.org/api-ref/compute/#list-flavors-with-details
	// https://github.com/openstack/nova/blob/master/nova/api/openstack/compute/flavors.py#L190-L223
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/flavors.py#L215-L216
	for _, tt := range []struct {
		name  string
		query string
		want  []string
	}{
		{name: "unfiltered", want: []string{tiny, small}},
		{name: "tiny boundaries", query: "?minRam=512&minDisk=1", want: []string{tiny, small}},
		{name: "signed minima", query: "?minRam=%2B512&minDisk=-1", want: []string{tiny, small}},
		{name: "zero minima", query: "?minRam=0&minDisk=0", want: []string{tiny, small}},
		{name: "disk minimum excludes tiny", query: "?minRam=512&minDisk=2", want: []string{small}},
		{name: "RAM minimum excludes tiny", query: "?minRam=513&minDisk=1", want: []string{small}},
		{name: "small boundaries", query: "?minRam=2048&minDisk=20", want: []string{small}},
		{name: "disk minimum excludes all", query: "?minRam=2048&minDisk=21"},
		{name: "RAM minimum excludes all", query: "?minRam=2049&minDisk=20"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodGet, path+"/detail"+tt.query, "", auth)

			require.Equal(t, http.StatusOK, status, "%s", body)
			requireJSON(t, headers)
			assert.Equal(t, "compute 2.1", headers.Get("OpenStack-API-Version"))
			assert.JSONEq(t, `{"flavors":[`+strings.Join(tt.want, ",")+`]}`, string(body))
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
