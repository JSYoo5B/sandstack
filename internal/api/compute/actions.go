package compute

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	appcompute "github.com/JSYoo5B/sandstack/internal/app/compute"
	domaincompute "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

const actionTimeLayout = "2006-01-02T15:04:05.000000"

type requestIDKey struct{}

type actionBody struct {
	Action       string  `json:"action"`
	InstanceUUID string  `json:"instance_uuid"`
	Message      *string `json:"message"`
	ProjectID    string  `json:"project_id"`
	RequestID    string  `json:"request_id"`
	StartTime    string  `json:"start_time"`
	UserID       string  `json:"user_id"`
}

func assignRequestIDs(newRequestID func() string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := newRequestID()
			w.Header().Set("X-Openstack-Request-Id", requestID)
			w.Header().Set("X-Compute-Request-Id", requestID)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, requestID)))
		})
	}
}

func requestID(r *http.Request) string {
	id, _ := r.Context().Value(requestIDKey{}).(string)
	return id
}

func (s requestScope) actionRequest() appcompute.ActionRequest {
	return appcompute.ActionRequest{ProjectID: s.projectID, UserID: s.userID, RequestID: s.requestID}
}

func (h handler) listServerActions(w http.ResponseWriter, r *http.Request, scope requestScope) {
	actions, err := h.service.ListActions(scope.projectID, chi.URLParam(r, "serverID"))
	if err != nil {
		writeActionError(w, err)
		return
	}
	response := struct {
		Actions []actionBody `json:"instanceActions"`
	}{Actions: make([]actionBody, len(actions))}
	for i, action := range actions {
		response.Actions[i] = describeAction(action)
	}
	writeJSON(w, http.StatusOK, response)
}

func (h handler) showServerAction(w http.ResponseWriter, r *http.Request, scope requestScope) {
	action, err := h.service.GetAction(scope.projectID, chi.URLParam(r, "serverID"), chi.URLParam(r, "requestID"))
	if err != nil {
		writeActionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Action actionBody `json:"instanceAction"`
	}{Action: describeAction(action)})
}

func describeAction(action domaincompute.Action) actionBody {
	return actionBody{
		Action:       action.Name,
		InstanceUUID: action.ServerID,
		ProjectID:    action.ProjectID,
		RequestID:    action.RequestID,
		StartTime:    action.StartTime.UTC().Format(actionTimeLayout),
		UserID:       action.UserID,
	}
}

func writeActionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, appcompute.ErrServerNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The server could not be found.")
	case errors.Is(err, appcompute.ErrActionNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The server action could not be found.")
	default:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to read the server actions.")
	}
}
