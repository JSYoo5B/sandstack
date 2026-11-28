package server_test

import (
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/images"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/members"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	consumerProjectID     = "8a3c5e2f0b9d4c71a6e4f2d1c0b9a876"
	memberSchemaPath      = "/image/v2/schemas/member"
	memberListSchemaPath  = "/image/v2/schemas/members"
	memberTimestampLayout = "2006-01-02T15:04:05Z"
)

// https://github.com/openstack/glance/blob/12.0.0/glance/tests/functional/v2/test_images.py#L3245-L3527
func TestGophercloudSharesPrivateGlanceImage(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client, err := openstack.NewImageV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	ownerID := routerProjectID(t, provider)
	baseline, err := images.Get(ctx, client, fixtureImageID).Extract()
	require.NoError(t, err)
	require.Equal(t, images.ImageVisibilityPrivate, baseline.Visibility)

	added, err := members.Create(ctx, client, fixtureImageID, consumerProjectID).Extract()
	require.NoError(t, err)
	_, duplicateErr := members.Create(ctx, client, fixtureImageID, consumerProjectID).Extract()
	read, err := members.Get(ctx, client, fixtureImageID, consumerProjectID).Extract()
	require.NoError(t, err)
	_, ownerAcceptErr := members.Update(ctx, client, fixtureImageID, consumerProjectID,
		members.UpdateOpts{Status: "accepted"}).Extract()
	pages, err := members.List(client, fixtureImageID).AllPages(ctx)
	require.NoError(t, err)
	listed, err := members.ExtractMembers(pages)
	require.NoError(t, err)

	assert.Equal(t, fixtureImageID, added.ImageID)
	assert.Equal(t, consumerProjectID, added.MemberID)
	assert.Equal(t, "pending", added.Status)
	assert.Equal(t, memberSchemaPath, added.Schema)
	assert.False(t, added.CreatedAt.IsZero())
	assert.Equal(t, added.CreatedAt, added.UpdatedAt)
	assert.True(t, gophercloud.ResponseCodeIs(duplicateErr, http.StatusConflict), "%v", duplicateErr)
	assert.Equal(t, added, read)
	assert.True(t, gophercloud.ResponseCodeIs(ownerAcceptErr, http.StatusForbidden), "%v", ownerAcceptErr)
	assert.Equal(t, []members.Member{*added}, listed)

	self, err := members.Create(ctx, client, fixtureImageID, ownerID).Extract()
	require.NoError(t, err)
	accepted, err := members.Update(ctx, client, fixtureImageID, ownerID,
		members.UpdateOpts{Status: "accepted"}).Extract()
	require.NoError(t, err)
	rejected, err := members.Update(ctx, client, fixtureImageID, ownerID,
		members.UpdateOpts{Status: "rejected"}).Extract()
	require.NoError(t, err)
	pending, err := members.Update(ctx, client, fixtureImageID, ownerID,
		members.UpdateOpts{Status: "pending"}).Extract()
	require.NoError(t, err)

	assert.Equal(t, "pending", self.Status)
	assert.Equal(t, "accepted", accepted.Status)
	assert.Equal(t, "rejected", rejected.Status)
	assert.Equal(t, "pending", pending.Status)
	assert.Equal(t, self.CreatedAt, pending.CreatedAt)

	require.NoError(t, members.Delete(ctx, client, fixtureImageID, consumerProjectID).ExtractErr())
	_, removedErr := members.Get(ctx, client, fixtureImageID, consumerProjectID).Extract()
	repeatErr := members.Delete(ctx, client, fixtureImageID, consumerProjectID).ExtractErr()
	pages, err = members.List(client, fixtureImageID).AllPages(ctx)
	require.NoError(t, err)
	remaining, err := members.ExtractMembers(pages)
	require.NoError(t, err)
	after, err := images.Get(ctx, client, fixtureImageID).Extract()
	require.NoError(t, err)

	assert.True(t, gophercloud.ResponseCodeIs(removedErr, http.StatusNotFound), "%v", removedErr)
	assert.True(t, gophercloud.ResponseCodeIs(repeatErr, http.StatusNotFound), "%v", repeatErr)
	require.Len(t, remaining, 1)
	assert.Equal(t, ownerID, remaining[0].MemberID)
	assert.Equal(t, baseline, after)
}

type wireMember struct {
	ImageID   string `json:"image_id"`
	MemberID  string `json:"member_id"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Schema    string `json:"schema"`
}

// https://github.com/openstack/glance/blob/12.0.0/glance/api/v2/image_members.py#L91-L322
func TestGlanceImageMembersWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	collection := imagesPath + "/" + fixtureImageID + "/members"
	resource := collection + "/" + consumerProjectID
	missingImage := imagesPath + "/00000000-0000-0000-0000-000000000000/members"
	status, _, body := request(t, instance, http.MethodGet, collection, "", nil)
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)

	status, headers, body := request(t, instance, http.MethodPost, collection,
		`{"member":"`+consumerProjectID+`"}`, auth)
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
		ImageID: fixtureImageID, MemberID: consumerProjectID, Status: "pending",
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

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
		status int
	}{
		{"duplicate", http.MethodPost, collection, `{"member":"` + consumerProjectID + `"}`, http.StatusConflict},
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
	status, _, body = request(t, instance, http.MethodPut, self, `{"status":"invalid"}`, auth)
	assert.Equal(t, http.StatusBadRequest, status, "%s", body)
	status, _, body = request(t, instance, http.MethodPut, self, `{"status":"accepted"}`, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	var accepted wireMember
	require.NoError(t, json.Unmarshal(body, &accepted))
	assert.Equal(t, "accepted", accepted.Status)

	status, _, body = request(t, instance, http.MethodDelete, resource, "", auth)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	assert.Empty(t, body)
	status, _, body = request(t, instance, http.MethodGet, resource, "", auth)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)

	status, _, body = request(t, instance, http.MethodPost, "/_sandstack/reset", "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	fresh := map[string]string{"X-Auth-Token": authenticate(t, instance).Token()}
	status, _, body = request(t, instance, http.MethodGet, collection, "", fresh)
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.JSONEq(t, `{"members":[],"schema":"`+memberListSchemaPath+`"}`, string(body))
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
