package mockapi

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// Activity results.
const (
	ActivityResultSuccess = "success"
	ActivityResultFailure = "failure"
)

// ActivityStateCancelled is the state of an activity after cancellation.
const ActivityStateCancelled = "cancelled" //nolint:misspell // The API's spelling.

// SetActivityResult sets the result of activities created from now on, e.g. by
// deploy or backup actions. The default is ActivityResultSuccess.
func (s *store) SetActivityResult(result string) {
	s.Lock()
	defer s.Unlock()
	s.activityResult = result
}

// ProjectActivities returns copies of a project's activities, newest first.
// The Payload is not copied.
func (s *store) ProjectActivities(projectID string) []*Activity {
	s.RLock()
	defer s.RUnlock()
	activities := make([]*Activity, 0, len(s.activities[projectID]))
	for _, a := range s.activities[projectID] {
		c := *a
		c.Environments = slices.Clone(a.Environments)
		c.Links = maps.Clone(a.Links)
		activities = append(activities, &c)
	}
	slices.SortFunc(activities, func(a, b *Activity) int { return -timeCompare(a.CreatedAt, b.CreatedAt) })
	return activities
}

// RespondWithActivity creates an activity and writes an action response that
// embeds it, for use in custom handlers. The activity is in progress until it is
// next read, when it completes with the result set by SetActivityResult.
func (h *Handler) RespondWithActivity(w http.ResponseWriter, projectID, environmentID, activityType string) {
	h.respondWithActivity(w, &Activity{Type: activityType, Project: projectID, Environments: []string{environmentID}})
}

// respondWithActivity adds an in-progress activity and writes an action response that embeds it.
func (h *Handler) respondWithActivity(w http.ResponseWriter, a *Activity) {
	h.respond(w, "OK", http.StatusOK, nil, a)
}

// respond writes a response in the API's envelope for writes and actions. It
// embeds the entity and the activity if they are not nil. The activity is added in progress.
func (h *Handler) respond(w http.ResponseWriter, status string, code int, entity any, a *Activity) {
	h.Lock()
	defer h.Unlock()
	embeddedValues := map[string]any{"activities": []*Activity{}}
	if a != nil {
		h.addActivity(a)
		embeddedValues["activities"] = []*Activity{a}
	}
	if entity != nil {
		embeddedValues["entity"] = entity
	}
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(apiResponse{Status: status, Code: code, Embedded: embeddedValues})
}

// statusError is the status of an error response.
const statusError = "error"

// apiResponse is the API's response format for writes, actions and errors.
type apiResponse struct {
	Status   string         `json:"status"`
	Code     int            `json:"code"`
	Title    string         `json:"title,omitempty"`
	Message  string         `json:"message,omitempty"`
	Detail   map[string]any `json:"detail,omitempty"`
	Embedded map[string]any `json:"_embedded,omitempty"`
}

// addActivity adds an in-progress activity. The caller must hold the write lock.
func (h *Handler) addActivity(a *Activity) {
	now := time.Now()
	a.ID = lowercaseAlphanumericID(16)
	a.State = "in_progress"
	a.pendingResult = h.activityResult
	if a.pendingResult == "" {
		a.pendingResult = ActivityResultSuccess
	}
	a.Description = "Mock " + a.Type
	a.Text = a.Description
	a.CreatedAt = now
	a.StartedAt = now
	a.UpdatedAt = now
	if h.activities == nil {
		h.activities = make(map[string]map[string]*Activity)
	}
	if h.activities[a.Project] == nil {
		h.activities[a.Project] = make(map[string]*Activity)
	}
	h.activities[a.Project][a.ID] = a
}

// writeError writes an error response in the API's format, with optional details.
func writeError(w http.ResponseWriter, code int, message string, detail map[string]any) {
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(apiResponse{
		Status: statusError, Code: code, Title: http.StatusText(code), Message: message, Detail: detail,
	})
}

// complete completes a created activity, as it is being read. The caller must hold the write lock.
func (a *Activity) complete() {
	if a.pendingResult != "" && a.State == "in_progress" {
		now := time.Now()
		a.State = "complete"
		a.Result = a.pendingResult
		a.CompletionPercent = 100
		a.CompletedAt = now
		a.UpdatedAt = now
	}
	a.pendingResult = ""
}

func (h *Handler) handleListProjectActivities(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	projectID := chi.URLParam(req, "project_id")
	var activities = make([]*Activity, 0, len(h.activities[projectID]))
	for _, a := range h.activities[projectID] {
		a.complete()
		activities = append(activities, a)
	}
	writeActivities(w, req, activities)
}

// writeActivities writes activities newest first, filtered by the query's type, state, result, starts_at and count
// parameters, like the API.
func writeActivities(w http.ResponseWriter, req *http.Request, activities []*Activity) {
	query := req.URL.Query()
	matches := func(key, value string) bool {
		return !query.Has(key) || slices.Contains(query[key], value)
	}
	var startsAt time.Time
	if v := query.Get("starts_at"); v != "" {
		var err error
		// The client does not encode the "+" of the time zone offset, so it is decoded as a space.
		if startsAt, err = time.Parse(time.RFC3339, strings.ReplaceAll(v, " ", "+")); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid starts_at", nil)
			return
		}
	}
	activities = slices.DeleteFunc(activities, func(a *Activity) bool {
		// The activity at starts_at is included: the CLI pages by the oldest activity's creation time.
		return !matches("type", a.Type) || !matches("state", a.State) || !matches("result", a.Result) ||
			(!startsAt.IsZero() && a.CreatedAt.After(startsAt))
	})
	slices.SortFunc(activities, func(a, b *Activity) int { return -timeCompare(a.CreatedAt, b.CreatedAt) })
	if count, err := strconv.Atoi(query.Get("count")); err == nil && count > 0 && count < len(activities) {
		activities = activities[:count]
	}
	_ = json.NewEncoder(w).Encode(activities)
}

func (h *Handler) handleGetProjectActivity(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	if a := h.activities[chi.URLParam(req, "project_id")][chi.URLParam(req, "id")]; a != nil {
		a.complete()
		_ = json.NewEncoder(w).Encode(a)
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

func (h *Handler) handleGetProjectActivityLog(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	a := h.activities[chi.URLParam(req, "project_id")][chi.URLParam(req, "id")]
	if a == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	a.complete()
	item, _ := json.Marshal(map[string]any{
		"_id": "1",
		"data": map[string]any{
			"timestamp": a.CreatedAt.Format(time.RFC3339),
			"message":   "Mock log for " + a.Type + "\n",
		},
	})
	_, _ = fmt.Fprintf(w, "%s\n{\"seal\":true}\n", item)
}

func (h *Handler) handleCancelProjectActivity(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	a := h.activities[chi.URLParam(req, "project_id")][chi.URLParam(req, "id")]
	if a == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if !a.cancellable() {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  "error",
			"code":    http.StatusBadRequest,
			"message": "The activity cannot be cancelled in its current state.", //nolint:misspell // The API's spelling.
		})
		return
	}
	now := time.Now()
	a.State = ActivityStateCancelled
	a.Result = ActivityResultFailure
	a.pendingResult = ""
	a.CompletedAt = now
	a.UpdatedAt = now
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "OK", "code": http.StatusOK})
}

func (h *Handler) handleListEnvironmentActivities(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	projectID := chi.URLParam(req, "project_id")
	environmentID := chi.URLParam(req, "environment_id")
	var activities = make([]*Activity, 0, len(h.activities[projectID]))
	for _, a := range h.activities[projectID] {
		if slices.Contains(a.Environments, environmentID) {
			a.complete()
			activities = append(activities, a)
		}
	}
	writeActivities(w, req, activities)
}

func (h *Handler) handleGetEnvironmentActivity(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	a := h.activities[chi.URLParam(req, "project_id")][chi.URLParam(req, "id")]
	if a == nil || !slices.Contains(a.Environments, chi.URLParam(req, "environment_id")) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	a.complete()
	_ = json.NewEncoder(w).Encode(a)
}
