package server_test

import (
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/imagedata"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/images"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/members"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	memberSchemaPath      = "/image/v2/schemas/member"
	memberListSchemaPath  = "/image/v2/schemas/members"
	memberTimestampLayout = "2006-01-02T15:04:05Z"
)

func imageClients(t *testing.T, providers ...*gophercloud.ProviderClient) []*gophercloud.ServiceClient {
	t.Helper()
	clients := make([]*gophercloud.ServiceClient, len(providers))
	for i, provider := range providers {
		client, err := openstack.NewImageV2(provider, gophercloud.EndpointOpts{
			Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
		})
		require.NoError(t, err)
		clients[i] = client
	}
	return clients
}

func listedImageIDs(t *testing.T, client *gophercloud.ServiceClient, opts images.ListOpts) []string {
	t.Helper()
	pages, err := images.List(client, opts).AllPages(t.Context())
	require.NoError(t, err)
	listed, err := images.ExtractImages(pages)
	require.NoError(t, err)
	ids := make([]string, len(listed))
	for i, image := range listed {
		ids[i] = image.ID
	}
	return ids
}

// https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/api/v2/image_members.py#L75-L345
// https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/db/utils.py#L44-L74
// https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/db/sqlalchemy/api.py#L588-L625
func TestGophercloudSharesGlanceImageWithAnotherProject(t *testing.T) {
	instance := startServerWithSecondFixture(t, "")
	ownerProvider, consumerProvider := authenticate(t, instance), authenticateSecond(t, instance)
	clients := imageClients(t, ownerProvider, consumerProvider)
	owner, consumer := clients[0], clients[1]
	consumerID := routerProjectID(t, consumerProvider)
	ctx := t.Context()
	private := images.ImageVisibilityPrivate
	created, err := images.Create(ctx, owner, images.CreateOpts{Name: "to share", Visibility: &private}).Extract()
	require.NoError(t, err)
	_, hiddenErr := images.Get(ctx, consumer, created.ID).Extract()
	_, privateMemberErr := members.Create(ctx, owner, created.ID, consumerID).Extract()

	assert.True(t, gophercloud.ResponseCodeIs(hiddenErr, http.StatusNotFound), "%v", hiddenErr)
	assert.True(t, gophercloud.ResponseCodeIs(privateMemberErr, http.StatusForbidden), "%v", privateMemberErr)

	shared, err := images.Update(ctx, owner, created.ID, images.UpdateOpts{
		images.UpdateVisibility{Visibility: images.ImageVisibilityShared},
	}).Extract()
	require.NoError(t, err)
	added, err := members.Create(ctx, owner, created.ID, consumerID).Extract()
	require.NoError(t, err)
	seen, err := images.Get(ctx, consumer, created.ID).Extract()
	require.NoError(t, err)
	own, err := members.Get(ctx, consumer, created.ID, consumerID).Extract()
	require.NoError(t, err)

	assert.Equal(t, images.ImageVisibilityShared, shared.Visibility)
	assert.Equal(t, "pending", added.Status)
	assert.Equal(t, created.ID, seen.ID)
	assert.Equal(t, routerProjectID(t, ownerProvider), seen.Owner)
	assert.Equal(t, added, own)
	assert.NotContains(t, listedImageIDs(t, consumer, images.ListOpts{}), created.ID)
	assert.Equal(t, []string{created.ID}, listedImageIDs(t, consumer, images.ListOpts{MemberStatus: images.ImageMemberStatusPending}))

	_, ownerAcceptErr := members.Update(ctx, owner, created.ID, consumerID, members.UpdateOpts{Status: "accepted"}).Extract()
	accepted, err := members.Update(ctx, consumer, created.ID, consumerID, members.UpdateOpts{Status: "accepted"}).Extract()
	require.NoError(t, err)
	_, consumerAddErr := members.Create(ctx, consumer, created.ID, "another-project").Extract()
	consumerRemoveErr := members.Delete(ctx, consumer, created.ID, consumerID).ExtractErr()
	_, consumerRenameErr := images.Update(ctx, consumer, created.ID, images.UpdateOpts{
		images.ReplaceImageName{NewName: "taken"},
	}).Extract()

	assert.True(t, gophercloud.ResponseCodeIs(ownerAcceptErr, http.StatusForbidden), "%v", ownerAcceptErr)
	assert.Equal(t, "accepted", accepted.Status)
	assert.True(t, gophercloud.ResponseCodeIs(consumerAddErr, http.StatusForbidden), "%v", consumerAddErr)
	assert.True(t, gophercloud.ResponseCodeIs(consumerRemoveErr, http.StatusForbidden), "%v", consumerRemoveErr)
	assert.True(t, gophercloud.ResponseCodeIs(consumerRenameErr, http.StatusForbidden), "%v", consumerRenameErr)
	assert.Contains(t, listedImageIDs(t, consumer, images.ListOpts{}), created.ID)

	require.NoError(t, members.Delete(ctx, owner, created.ID, consumerID).ExtractErr())
	_, removedErr := images.Get(ctx, consumer, created.ID).Extract()

	assert.True(t, gophercloud.ResponseCodeIs(removedErr, http.StatusNotFound), "%v", removedErr)
	assert.NotContains(t, listedImageIDs(t, consumer, images.ListOpts{MemberStatus: images.ImageMemberStatusAll}), created.ID)
}

// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/volume/api.py#L598-L686
// https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/policies/base.py#L36-L71
func TestGophercloudCreatesVolumeFromImageSharedByAnotherProject(t *testing.T) {
	instance := startServerWithSecondFixture(t, "")
	ownerProvider, consumerProvider := authenticate(t, instance), authenticateSecond(t, instance)
	clients := imageClients(t, ownerProvider, consumerProvider)
	owner, consumer := clients[0], clients[1]
	storage, err := openstack.NewBlockStorageV3(consumerProvider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	consumerID := routerProjectID(t, consumerProvider)
	_, fixtureErr := volumes.Create(ctx, storage, volumes.CreateOpts{Size: 1, ImageID: fixtureImageID}, nil).Extract()
	source, err := images.Create(ctx, owner, images.CreateOpts{
		Name: "shared source", ContainerFormat: "bare", DiskFormat: "qcow2",
	}).Extract()
	require.NoError(t, err)
	require.NoError(t, imagedata.Upload(ctx, owner, source.ID, strings.NewReader("shared bytes")).ExtractErr())
	_, err = members.Create(ctx, owner, source.ID, consumerID).Extract()
	require.NoError(t, err)

	created, err := volumes.Create(ctx, storage, volumes.CreateOpts{Size: 1, ImageID: source.ID}, nil).Extract()
	require.NoError(t, err)
	data, err := imagedata.Download(ctx, consumer, source.ID).Extract()
	require.NoError(t, err)
	defer data.Close()
	var bytes strings.Builder
	_, err = io.Copy(&bytes, data)
	require.NoError(t, err)

	assert.Equal(t, images.ImageVisibilityPrivate, fixtureVisibility(t, owner))
	assert.True(t, gophercloud.ResponseCodeIs(fixtureErr, http.StatusBadRequest), "%v", fixtureErr)
	assert.Equal(t, consumerID, created.TenantID)
	assert.Equal(t, "shared bytes", bytes.String())
}

func fixtureVisibility(t *testing.T, client *gophercloud.ServiceClient) images.ImageVisibility {
	t.Helper()
	fixture, err := images.Get(t.Context(), client, fixtureImageID).Extract()
	require.NoError(t, err)
	return fixture.Visibility
}

type wireMember struct {
	ImageID   string `json:"image_id"`
	MemberID  string `json:"member_id"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Schema    string `json:"schema"`
}

func createWireImage(t *testing.T, instance *httptest.Server, auth map[string]string, body string) string {
	t.Helper()
	status, _, data := request(t, instance, http.MethodPost, imagesPath, body, auth)
	require.Equal(t, http.StatusCreated, status, "%s", data)
	var created struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(data, &created))
	return created.ID
}

// https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/api/v2/image_members.py#L75-L436
func TestGlanceImageMembersWire(t *testing.T) {
	instance := startServerWithSecondFixture(t, "")
	provider := authenticate(t, instance)
	consumerID := routerProjectID(t, authenticateSecond(t, instance))
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	imageID := createWireImage(t, instance, auth, `{"name":"shared wire image"}`)
	collection := imagesPath + "/" + imageID + "/members"
	resource := collection + "/" + consumerID
	missingImage := imagesPath + "/00000000-0000-0000-0000-000000000000/members"
	status, _, body := request(t, instance, http.MethodGet, collection, "", nil)
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)

	status, headers, body := request(t, instance, http.MethodPost, collection,
		`{"member":"`+consumerID+`"}`, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &fields))
	var created wireMember
	require.NoError(t, json.Unmarshal(body, &created))

	assert.ElementsMatch(t,
		[]string{"image_id", "member_id", "status", "created_at", "updated_at", "schema"},
		slices.Collect(maps.Keys(fields)))
	assert.Equal(t, wireMember{
		ImageID: imageID, MemberID: consumerID, Status: "pending",
		CreatedAt: created.CreatedAt, UpdatedAt: created.CreatedAt, Schema: memberSchemaPath,
	}, created)
	_, err := time.Parse(memberTimestampLayout, created.CreatedAt)
	assert.NoError(t, err)

	status, headers, body = request(t, instance, http.MethodGet, collection, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	var list struct {
		Members []wireMember `json:"members"`
		Schema  string       `json:"schema"`
	}
	require.NoError(t, json.Unmarshal(body, &list))

	assert.Equal(t, []wireMember{created}, list.Members)
	assert.Equal(t, memberListSchemaPath, list.Schema)

	fixtureMembers := imagesPath + "/" + fixtureImageID + "/members"
	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
		status int
	}{
		{"duplicate", http.MethodPost, collection, `{"member":"` + consumerID + `"}`, http.StatusConflict},
		{"missing member", http.MethodPost, collection, `{}`, http.StatusBadRequest},
		{"empty member", http.MethodPost, collection, `{"member":""}`, http.StatusBadRequest},
		{"non-object body", http.MethodPost, collection, `["member"]`, http.StatusBadRequest},
		{"missing image create", http.MethodPost, missingImage, `{"member":"x"}`, http.StatusNotFound},
		{"missing image list", http.MethodGet, missingImage, "", http.StatusNotFound},
		{"missing member get", http.MethodGet, collection + "/absent", "", http.StatusNotFound},
		{"missing member update", http.MethodPut, collection + "/absent", `{"status":"accepted"}`, http.StatusNotFound},
		{"missing member delete", http.MethodDelete, collection + "/absent", "", http.StatusNotFound},
		{"owner accepts", http.MethodPut, resource, `{"status":"accepted"}`, http.StatusForbidden},
		{"owner sets invalid", http.MethodPut, resource, `{"status":"invalid"}`, http.StatusForbidden},
		{"missing status", http.MethodPut, resource, `{}`, http.StatusBadRequest},
		{"private image create", http.MethodPost, fixtureMembers, `{"member":"` + consumerID + `"}`, http.StatusForbidden},
		{"private image list", http.MethodGet, fixtureMembers, "", http.StatusForbidden},
		{"private image get", http.MethodGet, fixtureMembers + "/" + consumerID, "", http.StatusNotFound},
		{"private image update", http.MethodPut, fixtureMembers + "/" + consumerID, `{"status":"accepted"}`, http.StatusForbidden},
		{"private image delete", http.MethodDelete, fixtureMembers + "/" + consumerID, "", http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, _, body := request(t, instance, tc.method, tc.path, tc.body, auth)

			assert.Equal(t, tc.status, status, "%s", body)
		})
	}

	ownerID := routerProjectID(t, provider)
	self := collection + "/" + ownerID
	status, _, body = request(t, instance, http.MethodPost, collection, `{"member":"`+ownerID+`"}`, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	status, _, body = request(t, instance, http.MethodPut, self, `{"status":"accepted"}`, auth)
	assert.Equal(t, http.StatusForbidden, status, "%s", body)

	status, _, body = request(t, instance, http.MethodDelete, resource, "", auth)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	assert.Empty(t, body)
	status, _, body = request(t, instance, http.MethodGet, resource, "", auth)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)

	status, _, body = request(t, instance, http.MethodPost, "/_sandstack/reset", "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	fresh := map[string]string{"X-Auth-Token": authenticate(t, instance).Token()}
	status, _, body = request(t, instance, http.MethodGet, collection, "", fresh)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)
}

// https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/db/__init__.py#L67-L77
// https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/api/v2/policy.py#L176-L196
// https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/api/v2/images.py#L1582-L1597
func TestGlanceCrossProjectImageWire(t *testing.T) {
	instance := startServerWithSecondFixture(t, "")
	ownerProvider, consumerProvider := authenticate(t, instance), authenticateSecond(t, instance)
	owner := map[string]string{"X-Auth-Token": ownerProvider.Token()}
	consumer := map[string]string{"X-Auth-Token": consumerProvider.Token()}
	ownerID, consumerID := routerProjectID(t, ownerProvider), routerProjectID(t, consumerProvider)
	sharedID := createWireImage(t, instance, owner, `{"name":"shared","container_format":"bare","disk_format":"qcow2"}`)
	privateID := createWireImage(t, instance, owner, `{"name":"private","visibility":"private"}`)
	communityID := createWireImage(t, instance, owner, `{"name":"community","visibility":"community"}`)
	status, _, body := request(t, instance, http.MethodPost, imagesPath+"/"+sharedID+"/members",
		`{"member":"`+consumerID+`"}`, owner)
	require.Equal(t, http.StatusOK, status, "%s", body)
	shared := imagesPath + "/" + sharedID
	private := imagesPath + "/" + privateID
	rename := `[{"op":"replace","path":"/name","value":"taken"}]`
	patch := map[string]string{"X-Auth-Token": consumerProvider.Token(), "Content-Type": "application/openstack-images-v2.1-json-patch"}
	upload := map[string]string{"X-Auth-Token": consumerProvider.Token(), "Content-Type": "application/octet-stream"}

	for _, tc := range []struct {
		name    string
		method  string
		path    string
		body    string
		headers map[string]string
		status  int
	}{
		{"read private", http.MethodGet, private, "", consumer, http.StatusNotFound},
		{"rename private", http.MethodPatch, private, rename, patch, http.StatusNotFound},
		{"delete private", http.MethodDelete, private, "", consumer, http.StatusNotFound},
		{"upload private", http.MethodPut, private + "/file", "data", upload, http.StatusNotFound},
		{"download private", http.MethodGet, private + "/file", "", consumer, http.StatusNotFound},
		{"list private members", http.MethodGet, private + "/members", "", consumer, http.StatusNotFound},
		{"read shared", http.MethodGet, shared, "", consumer, http.StatusOK},
		{"rename shared", http.MethodPatch, shared, rename, patch, http.StatusForbidden},
		{"delete shared", http.MethodDelete, shared, "", consumer, http.StatusForbidden},
		{"upload shared", http.MethodPut, shared + "/file", "data", upload, http.StatusForbidden},
		{"add member", http.MethodPost, shared + "/members", `{"member":"` + ownerID + `"}`, consumer, http.StatusForbidden},
		{"remove own member", http.MethodDelete, shared + "/members/" + consumerID, "", consumer, http.StatusForbidden},
		{"read other member", http.MethodGet, shared + "/members/" + ownerID, "", consumer, http.StatusNotFound},
		{"owner accepts", http.MethodPut, shared + "/members/" + consumerID, `{"status":"accepted"}`, owner, http.StatusForbidden},
		{"rename community", http.MethodPatch, imagesPath + "/" + communityID, rename, patch, http.StatusForbidden},
		{"invalid member status", http.MethodGet, imagesPath + "?member_status=maybe", "", consumer, http.StatusBadRequest},
		{"invalid visibility", http.MethodGet, imagesPath + "?visibility=hidden", "", consumer, http.StatusBadRequest},
		{"create public", http.MethodPost, imagesPath, `{"name":"p","visibility":"public"}`, consumer, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, _, body := request(t, instance, tc.method, tc.path, tc.body, tc.headers)

			assert.Equal(t, tc.status, status, "%s", body)
		})
	}

	listIDs := func(query string) []string {
		t.Helper()
		status, _, body := request(t, instance, http.MethodGet, imagesPath+query, "", consumer)
		require.Equal(t, http.StatusOK, status, "%s", body)
		var listed struct {
			Images []struct {
				ID string `json:"id"`
			} `json:"images"`
		}
		require.NoError(t, json.Unmarshal(body, &listed))
		ids := make([]string, len(listed.Images))
		for i, image := range listed.Images {
			ids[i] = image.ID
		}
		return ids
	}
	status, _, body = request(t, instance, http.MethodGet, shared+"/members", "", consumer)
	require.Equal(t, http.StatusOK, status, "%s", body)
	var members struct {
		Members []wireMember `json:"members"`
	}
	require.NoError(t, json.Unmarshal(body, &members))
	status, _, body = request(t, instance, http.MethodPut, shared+"/members/"+consumerID, `{"status":"rejected"}`, consumer)
	require.Equal(t, http.StatusOK, status, "%s", body)

	require.Len(t, members.Members, 1)
	assert.Equal(t, consumerID, members.Members[0].MemberID)
	assert.Empty(t, listIDs(""))
	assert.Equal(t, []string{sharedID}, listIDs("?member_status=rejected"))
	assert.Equal(t, []string{communityID}, listIDs("?visibility=community"))
	assert.Equal(t, []string{communityID, sharedID}, listIDs("?visibility=all&member_status=all"))
}

// https://github.com/openstack/glance/blob/12.0.0/glance/api/v2/schemas.py#L64-L68
func TestGlanceMemberSchemasWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}

	status, _, body := request(t, instance, http.MethodGet, memberSchemaPath, "", nil)
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	status, headers, singular := request(t, instance, http.MethodGet, memberSchemaPath, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", singular)
	requireJSON(t, headers)
	status, headers, collection := request(t, instance, http.MethodGet, memberListSchemaPath, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", collection)
	requireJSON(t, headers)

	var member struct {
		Name       string                     `json:"name"`
		Properties map[string]json.RawMessage `json:"properties"`
		Links      json.RawMessage            `json:"links"`
	}
	require.NoError(t, json.Unmarshal(singular, &member))
	var members struct {
		Name       string `json:"name"`
		Properties struct {
			Members struct {
				Type  string          `json:"type"`
				Items json.RawMessage `json:"items"`
			} `json:"members"`
		} `json:"properties"`
		Links []schemaLinkBody `json:"links"`
	}
	require.NoError(t, json.Unmarshal(collection, &members))

	assert.Equal(t, "member", member.Name)
	assert.ElementsMatch(t,
		[]string{"image_id", "member_id", "status", "created_at", "updated_at", "schema"},
		slices.Collect(maps.Keys(member.Properties)))
	assert.JSONEq(t, `{"type":"string","enum":["pending","accepted","rejected"]}`,
		string(member.Properties["status"]))
	assert.Nil(t, member.Links)
	assert.Equal(t, "members", members.Name)
	assert.Equal(t, "array", members.Properties.Members.Type)
	assert.JSONEq(t, string(singular), string(members.Properties.Members.Items))
	assert.Equal(t, []schemaLinkBody{{Rel: "describedby", Href: "{schema}"}}, members.Links)
}

type schemaLinkBody struct {
	Rel  string `json:"rel"`
	Href string `json:"href"`
}
