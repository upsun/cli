package mockapi

import (
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"
)

// backupWithLinks returns a copy of a backup with links to view, restore and
// delete it, unless it has links. Like the API, only created backups can be restored or deleted.
func backupWithLinks(projectID string, b *Backup) *Backup {
	c := *b
	if c.Links == nil {
		path := "/projects/" + url.PathEscape(projectID) + "/environments/" + url.PathEscape(b.EnvironmentID) +
			"/backups/" + url.PathEscape(b.ID)
		c.Links = MakeHALLinks("self=" + path)
		if b.Status == "CREATED" {
			c.Links["#restore"] = HALLink{HREF: path + "/restore"}
			c.Links["#delete"] = HALLink{HREF: path}
		}
	} else {
		c.Links = maps.Clone(b.Links)
	}
	return &c
}

// findBackup returns a backup on an environment. The caller must hold a lock.
func (h *Handler) findBackup(req *http.Request) *Backup {
	b := h.projectBackups[chi.URLParam(req, "project_id")][chi.URLParam(req, "backup_id")]
	if b == nil || b.EnvironmentID != chi.URLParam(req, "environment_id") {
		return nil
	}
	return b
}

func (h *Handler) handleGetBackup(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	b := h.findBackup(req)
	if b == nil {
		writeError(w, http.StatusNotFound, "Not found", nil)
		return
	}
	_ = json.NewEncoder(w).Encode(backupWithLinks(chi.URLParam(req, "project_id"), b))
}

// handleDeleteBackup schedules the deletion of a backup: like the API, it is
// marked as being deleted, and remains until the activity runs (not modeled).
func (h *Handler) handleDeleteBackup(w http.ResponseWriter, req *http.Request) {
	projectID := chi.URLParam(req, "project_id")
	h.Lock()
	b := h.findBackup(req)
	if b != nil {
		c := *b
		c.Status = "DELETING"
		c.UpdatedAt = time.Now()
		h.projectBackups[projectID][b.ID] = &c
	}
	h.Unlock()
	if b == nil {
		writeError(w, http.StatusNotFound, "Not found", nil)
		return
	}
	h.respond(w, "deleted", http.StatusOK, nil, &Activity{
		Type: "environment.backup.delete", Project: projectID, Environments: []string{b.EnvironmentID},
	})
}

// handleRestoreBackup records a request to restore a backup. The activity
// belongs to the target environment, which is not created if it does not exist.
func (h *Handler) handleRestoreBackup(w http.ResponseWriter, req *http.Request) {
	projectID := chi.URLParam(req, "project_id")
	// An empty body uses the defaults.
	body := map[string]any{}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "Invalid JSON", nil)
		return
	}
	if branchFrom, ok := body["branch_from"].(string); ok && h.findEnvironment(projectID, branchFrom) == nil {
		writeError(w, http.StatusBadRequest, "Invalid restore options",
			map[string]any{"branch_from": "Environment not found: " + branchFrom})
		return
	}
	h.Lock()
	b := h.findBackup(req)
	if b != nil {
		if h.backupRestores == nil {
			h.backupRestores = make(map[string][]map[string]any)
		}
		h.backupRestores[projectID] = append(h.backupRestores[projectID], body)
	}
	h.Unlock()
	if b == nil {
		writeError(w, http.StatusNotFound, "Not found", nil)
		return
	}
	target := b.EnvironmentID
	if name, ok := body["environment_name"].(string); ok && name != "" {
		target = name
	}
	h.RespondWithActivity(w, projectID, target, "environment.restore")
}

// BackupRestores returns the bodies of requests to restore a project's backups.
func (s *store) BackupRestores(projectID string) []map[string]any {
	s.RLock()
	defer s.RUnlock()
	return slices.Clone(s.backupRestores[projectID])
}

// ProjectBackups returns copies of a project's backups, sorted by ID.
func (s *store) ProjectBackups(projectID string) []*Backup {
	s.RLock()
	defer s.RUnlock()
	backups := make([]*Backup, 0, len(s.projectBackups[projectID]))
	for _, id := range slices.Sorted(maps.Keys(s.projectBackups[projectID])) {
		c := *s.projectBackups[projectID][id]
		backups = append(backups, &c)
	}
	return backups
}
