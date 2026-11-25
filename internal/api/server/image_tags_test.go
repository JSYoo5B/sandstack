package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudReplacesAndFiltersImageTags(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client, err := openstack.NewImageV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	read := func() *images.Image {
		t.Helper()
		image, err := images.Get(ctx, client, fixtureImageID).Extract()
		require.NoError(t, err)
		return image
	}
	list := func(opts images.ListOpts) []images.Image {
		t.Helper()
		pages, err := images.List(client, opts).AllPages(ctx)
		require.NoError(t, err)
		items, err := images.ExtractImages(pages)
		require.NoError(t, err)
		return items
	}
	replace := func(tags []string) (*images.Image, error) {
		t.Helper()
		return images.Update(ctx, client, fixtureImageID, images.UpdateOpts{
			images.ReplaceImageTags{NewTags: tags},
		}).Extract()
	}
	baseline := read()
	require.Equal(t, []string{}, baseline.Tags)
	const blue = "blue + lab"
	const ownerTag = "scope:internal"
	selected := images.ListOpts{Name: baseline.Name, Status: baseline.Status, Tags: []string{ownerTag, blue}}
	assert.Empty(t, list(selected))

	tagged, err := replace([]string{ownerTag, blue, ownerTag})

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{blue, ownerTag}, tagged.Tags)
	want := *baseline
	want.Tags, want.UpdatedAt = tagged.Tags, tagged.UpdatedAt
	assert.Equal(t, &want, tagged)
	assert.Equal(t, tagged, read())
	assert.Equal(t, []images.Image{*tagged}, list(selected))
	assert.Equal(t, []images.Image{*tagged}, list(images.ListOpts{Tags: []string{blue}}))
	assert.Empty(t, list(images.ListOpts{Tags: []string{blue, "missing-tag"}}))
	assert.Empty(t, list(images.ListOpts{Tags: []string{"BLUE + LAB"}}))
	assert.Empty(t, list(images.ListOpts{Tags: []string{blue}, Status: images.ImageStatusDeactivated}))

	_, err = replace(nil)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusBadRequest), "nil tags: %v", err)
	assert.Equal(t, tagged, read())

	replaced, err := replace([]string{"new-tag"})
	require.NoError(t, err)
	assert.Equal(t, []string{"new-tag"}, replaced.Tags)
	assert.Equal(t, replaced, read())
	assert.Empty(t, list(selected))
	assert.Equal(t, []images.Image{*replaced}, list(images.ListOpts{Tags: []string{"new-tag"}}))

	cleared, err := replace([]string{})

	require.NoError(t, err)
	assert.Equal(t, []string{}, cleared.Tags)
	assert.Equal(t, cleared, read())
	assert.Empty(t, list(images.ListOpts{Tags: []string{"new-tag"}}))
	assert.Equal(t, []images.Image{*cleared}, list(images.ListOpts{Name: baseline.Name}))
}

func TestImageTagPatchAndFiltersWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	auth := map[string]string{
		"X-Auth-Token": provider.Token(),
		"Content-Type": "application/openstack-images-v2.1-json-patch",
	}
	path := imagesPath + "/" + fixtureImageID
	patch := func(tags any) string {
		t.Helper()
		value, err := json.Marshal(tags)
		require.NoError(t, err)
		return `[{"op":"replace","path":"/tags","value":` + string(value) + `}]`
	}
	type tagImage struct {
		ID   string   `json:"id"`
		Tags []string `json:"tags"`
	}
	readTags := func(body []byte) []string {
		t.Helper()
		var image tagImage
		require.NoError(t, json.Unmarshal(body, &image))
		require.Equal(t, fixtureImageID, image.ID)
		require.NotNil(t, image.Tags)
		return image.Tags
	}
	const firstTag = "scope:internal"
	const secondTag = "space + & tag"
	longTag := strings.Repeat("한", 255)
	atLimit := []string{firstTag, secondTag, longTag}
	for i := len(atLimit); i < 128; i++ {
		atLimit = append(atLimit, fmt.Sprintf("entry-%03d", i))
	}

	// https://docs.openstack.org/api-ref/image/v2/#update-an-image
	status, headers, body := request(t, instance, http.MethodPatch, path, patch(atLimit), auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.ElementsMatch(t, atLimit, readTags(body))
	before := string(body)
	tooMany := make([]string, 129)
	for i := range tooMany {
		tooMany[i] = firstTag
	}
	for _, rejected := range []struct {
		tags any
		code int
	}{
		{nil, http.StatusBadRequest},
		{[]any{firstTag, nil}, http.StatusBadRequest},
		{[]any{firstTag, 1}, http.StatusBadRequest},
		{firstTag, http.StatusBadRequest},
		{[]string{longTag + "한"}, http.StatusBadRequest},
		{tooMany, http.StatusRequestEntityTooLarge},
	} {
		status, _, body := request(t, instance, http.MethodPatch, path, patch(rejected.tags), auth)
		require.Equal(t, rejected.code, status, "%s", body)
	}
	status, headers, body = request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, before, string(body))

	// https://docs.openstack.org/api-ref/image/v2/#list-images
	for _, test := range []struct {
		tags  []string
		match bool
	}{
		{[]string{secondTag, firstTag, secondTag}, true},
		{[]string{firstTag, "missing-tag"}, false},
	} {
		query := url.Values{"tag": test.tags, "status": {"active"}}
		status, headers, body := request(t, instance, http.MethodGet, imagesPath+"?"+query.Encode(), "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		var page struct {
			First  string            `json:"first"`
			Images []json.RawMessage `json:"images"`
		}
		require.NoError(t, json.Unmarshal(body, &page))
		first, err := url.Parse(page.First)
		require.NoError(t, err)
		assert.Equal(t, imagesPath, first.Path)
		assert.Equal(t, query, first.Query())
		require.NotNil(t, page.Images)
		if test.match {
			require.Len(t, page.Images, 1)
			assert.ElementsMatch(t, atLimit, readTags(page.Images[0]))
		} else {
			assert.Empty(t, page.Images)
		}
	}
}
