package image

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/image"
	domain "github.com/JSYoo5B/sandstack/internal/domain/image"
)

const (
	memberSchemaPath     = Path + "/schemas/member"
	memberListSchemaPath = Path + "/schemas/members"
	maxMemberBody        = 64 * 1024
	memberTimeLayout     = "2006-01-02T15:04:05Z"
)

var (
	errMemberBody   = errors.New(`expected a member in the form {"member": "<project_id>"}`)
	errMemberStatus = errors.New(`expected a status in the form {"status": "<status>"}`)
)

type memberBody struct {
	ImageID   string `json:"image_id"`
	MemberID  string `json:"member_id"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Schema    string `json:"schema"`
}

func describeMember(member domain.Member) memberBody {
	return memberBody{
		ImageID:   member.ImageID,
		MemberID:  member.MemberID,
		Status:    member.Status,
		CreatedAt: member.CreatedAt.UTC().Format(memberTimeLayout),
		UpdatedAt: member.UpdatedAt.UTC().Format(memberTimeLayout),
		Schema:    memberSchemaPath,
	}
}

func (h handler) listMembers(w http.ResponseWriter, r *http.Request, projectID string) {
	members, err := h.service.ListMembers(projectID, chi.URLParam(r, "imageID"))
	if err != nil {
		writeMemberError(w, err)
		return
	}
	body := struct {
		Members []memberBody `json:"members"`
		Schema  string       `json:"schema"`
	}{Members: make([]memberBody, 0, len(members)), Schema: memberListSchemaPath}
	for _, member := range members {
		body.Members = append(body.Members, describeMember(member))
	}
	writeJSON(w, http.StatusOK, body)
}

func (h handler) addMember(w http.ResponseWriter, r *http.Request, projectID string) {
	fields, ok := readMemberRequest(w, r)
	if !ok {
		return
	}
	var memberID *string
	if raw, found := fields["member"]; !found || json.Unmarshal(raw, &memberID) != nil ||
		memberID == nil || *memberID == "" {
		http.Error(w, errMemberBody.Error(), http.StatusBadRequest)
		return
	}

	member, err := h.service.AddMember(projectID, chi.URLParam(r, "imageID"), *memberID)
	if err != nil {
		writeMemberError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, describeMember(member))
}

func (h handler) showMember(w http.ResponseWriter, r *http.Request, projectID string) {
	member, err := h.service.GetMember(projectID, chi.URLParam(r, "imageID"), chi.URLParam(r, "memberID"))
	if err != nil {
		writeMemberError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, describeMember(member))
}

func (h handler) updateMember(w http.ResponseWriter, r *http.Request, projectID string) {
	fields, ok := readMemberRequest(w, r)
	if !ok {
		return
	}
	raw, found := fields["status"]
	if !found {
		http.Error(w, errMemberStatus.Error(), http.StatusBadRequest)
		return
	}
	var status string
	_ = json.Unmarshal(raw, &status)

	member, err := h.service.UpdateMember(
		projectID, chi.URLParam(r, "imageID"), chi.URLParam(r, "memberID"), status,
	)
	if err != nil {
		writeMemberError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, describeMember(member))
}

func (h handler) removeMember(w http.ResponseWriter, r *http.Request, projectID string) {
	err := h.service.RemoveMember(projectID, chi.URLParam(r, "imageID"), chi.URLParam(r, "memberID"))
	if err != nil {
		writeMemberError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func readMemberRequest(w http.ResponseWriter, r *http.Request) (map[string]json.RawMessage, bool) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxMemberBody))
	var sizeError *http.MaxBytesError
	if errors.As(err, &sizeError) {
		http.Error(w, "The image member request is too large.", http.StatusRequestEntityTooLarge)
		return nil, false
	}
	var fields map[string]json.RawMessage
	if err != nil || !utf8.Valid(data) || json.Unmarshal(data, &fields) != nil || fields == nil {
		http.Error(w, "Body expected in request.", http.StatusBadRequest)
		return nil, false
	}
	return fields, true
}

func writeMemberError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, app.ErrImageNotFound):
		http.Error(w, "Image not found.", http.StatusNotFound)
	case errors.Is(err, app.ErrMemberNotFound):
		http.Error(w, "The member was not found in the member list of the image.", http.StatusNotFound)
	case errors.Is(err, app.ErrMemberExists):
		http.Error(w, "The member is already associated with the image.", http.StatusConflict)
	case errors.Is(err, app.ErrMemberLimit):
		http.Error(w, "The image member count exceeds the limit of 128.", http.StatusRequestEntityTooLarge)
	case errors.Is(err, app.ErrMemberForbidden):
		http.Error(w, "Not allowed to update members for the image.", http.StatusForbidden)
	case errors.Is(err, app.ErrMemberStatus):
		http.Error(w, `Status must be "pending", "accepted" or "rejected".`, http.StatusBadRequest)
	default:
		http.Error(w, "Unable to process the image member request.", http.StatusInternalServerError)
	}
}
