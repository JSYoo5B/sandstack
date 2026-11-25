package server_test

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/imagedata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageSingleByteRangeWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	path := imagesPath + "/" + fixtureImageID + "/file"
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	status, _, baseline := request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", baseline)
	require.Len(t, baseline, fixtureImageDataSize)
	requestRange := func(value string) (int, http.Header, []byte) {
		t.Helper()
		return request(t, instance, http.MethodGet, path, "", map[string]string{
			"X-Auth-Token": provider.Token(),
			"Range":        value,
		})
	}
	const tooLargeForUint64 = "18446744073709551616"

	// https://github.com/openstack/glance/blob/master/glance/api/v2/image_data.py#L608-L651
	for _, example := range []struct {
		name  string
		value string
		start int
		end   int
	}{
		{"closed range", "bytes=0-31", 0, 32},
		{"open range", "bytes=1024-", 1024, fixtureImageDataSize},
		{"suffix range", "bytes=-17", fixtureImageDataSize - 17, fixtureImageDataSize},
		{"truncated range", "bytes=1789-9000", 1789, fixtureImageDataSize},
		{"suffix covers full image", "bytes=-9999", 0, fixtureImageDataSize},
		{"oversized end value", "bytes=1789-" + tooLargeForUint64, 1789, fixtureImageDataSize},
		{"oversized suffix length", "bytes=-" + tooLargeForUint64, 0, fixtureImageDataSize},
		{"case-insensitive range unit", "BYTES=0-0", 0, 1},
		{"last byte", "bytes=1791-1791", 1791, fixtureImageDataSize},
	} {
		t.Run(example.name, func(t *testing.T) {
			status, headers, body := requestRange(example.value)

			require.Equal(t, http.StatusPartialContent, status, "%s", body)
			assert.Equal(t, "application/octet-stream", headers.Get("Content-Type"))
			assert.Equal(t, fixtureImageMD5, headers.Get("Content-MD5"))
			assert.Equal(t, strconv.Itoa(example.end-example.start), headers.Get("Content-Length"))
			contentRange := fmt.Sprintf("bytes %d-%d/%d", example.start, example.end-1, fixtureImageDataSize)
			assert.Equal(t, contentRange, headers.Get("Content-Range"))
			assert.Equal(t, baseline[example.start:example.end], body)
		})
	}

	// https://github.com/openstack/glance/blob/master/glance/common/wsgi.py#L292-L314
	for _, invalid := range []struct {
		name   string
		value  string
		status int
	}{
		{"multiple ranges", "bytes=0-3,7-9", http.StatusBadRequest},
		{"unknown range unit", "items=0-3", http.StatusRequestedRangeNotSatisfiable},
		{"reversed range", "bytes=9-3", http.StatusRequestedRangeNotSatisfiable},
		{"start past image", "bytes=1792-", http.StatusRequestedRangeNotSatisfiable},
		{"oversized start value", "bytes=" + tooLargeForUint64 + "-", http.StatusRequestedRangeNotSatisfiable},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			status, headers, body := requestRange(invalid.value)
			assert.Equal(t, invalid.status, status, "%s", body)
			if invalid.status == http.StatusRequestedRangeNotSatisfiable {
				assert.Equal(t, "bytes */"+strconv.Itoa(fixtureImageDataSize), headers.Get("Content-Range"))
			}
		})
	}
	repeated, err := http.NewRequestWithContext(t.Context(), http.MethodGet, instance.URL+path, nil)
	require.NoError(t, err)
	repeated.Header.Set("X-Auth-Token", provider.Token())
	repeated.Header.Add("Range", "bytes=0-3")
	repeated.Header.Add("Range", "bytes=7-9")
	response, err := instance.Client().Do(repeated)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	require.NoError(t, response.Body.Close())

	status, headers, body := request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.Empty(t, headers.Get("Content-Range"))
	assert.Equal(t, baseline, body)

	deactivate := "/_sandstack/image/images/" + fixtureImageID + "/deactivated"
	status, _, body = request(t, instance, http.MethodPut, deactivate, "", map[string]string{
		"X-SandStack-Admin-Token": adminToken,
	})
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	status, _, body = requestRange("bytes=0-31")
	assert.Equal(t, http.StatusForbidden, status, "%s", body)
}

func TestGophercloudReassemblesImageFromByteRanges(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client, err := openstack.NewImageV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	consume := func(reader io.ReadCloser) []byte {
		t.Helper()
		require.NotNil(t, reader)
		data, err := io.ReadAll(reader)
		closeErr := reader.Close()
		require.NoError(t, err)
		require.NoError(t, closeErr)
		return data
	}
	reader, err := imagedata.Download(t.Context(), client, fixtureImageID).Extract()
	require.NoError(t, err)
	full := consume(reader)
	require.Len(t, full, fixtureImageDataSize)
	path := client.ServiceURL("images", fixtureImageID, "file")

	// The pinned SDK's full download helper does not send or accept Range.
	// Use the catalog-derived service client for authenticated partial reads.
	var rebuilt []byte
	for _, part := range []struct {
		value string
		start int
		end   int
	}{
		{"bytes=0-111", 0, 112},
		{"bytes=112-1284", 112, 1285},
		{"bytes=1285-", 1285, fixtureImageDataSize},
	} {
		response, err := client.Get(t.Context(), path, nil, &gophercloud.RequestOpts{
			MoreHeaders:      map[string]string{"Range": part.value},
			OkCodes:          []int{http.StatusPartialContent},
			KeepResponseBody: true,
		})
		require.NoError(t, err)
		require.NotNil(t, response)
		assert.Equal(t, http.StatusPartialContent, response.StatusCode)
		assert.Equal(t, fixtureImageMD5, response.Header.Get("Content-MD5"))
		contentRange := fmt.Sprintf("bytes %d-%d/%d", part.start, part.end-1, fixtureImageDataSize)
		assert.Equal(t, contentRange, response.Header.Get("Content-Range"))
		result := consume(response.Body)
		assert.Len(t, result, part.end-part.start)
		assert.Equal(t, full[part.start:part.end], result)
		rebuilt = append(rebuilt, result...)
	}
	assert.Equal(t, full, rebuilt)
}
