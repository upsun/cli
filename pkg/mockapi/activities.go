package mockapi

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
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
	h.Lock()
	defer h.Unlock()
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
	_ = json.NewEncoder(w).Encode(embedded("activities", []*Activity{a}))
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
	// Sort activities in descending order by created date.
	slices.SortFunc(activities, func(a, b *Activity) int { return -timeCompare(a.CreatedAt, b.CreatedAt) })
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
	// Sort activities in descending order by created date.
	slices.SortFunc(activities, func(a, b *Activity) int { return -timeCompare(a.CreatedAt, b.CreatedAt) })
	_ = json.NewEncoder(w).Encode(activities)
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
