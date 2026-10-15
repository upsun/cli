package mockapi

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/url"
	"slices"

	"github.com/go-chi/chi/v5"
)

// backupWithLinks returns a copy of a backup with links to view, restore and delete it, unless it has links.
func backupWithLinks(projectID string, b *Backup) *Backup {
	c := *b
	if c.Links == nil {
		path := "/projects/" + url.PathEscape(projectID) + "/environments/" + url.PathEscape(b.EnvironmentID) +
			"/backups/" + url.PathEscape(b.ID)
		c.Links = MakeHALLinks("self="+path, "#restore="+path+"/restore", "#delete="+path)
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
		w.WriteHeader(http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(backupWithLinks(chi.URLParam(req, "project_id"), b))
}

func (h *Handler) handleDeleteBackup(w http.ResponseWriter, req *http.Request) {
	projectID := chi.URLParam(req, "project_id")
	h.Lock()
	b := h.findBackup(req)
	if b != nil {
		delete(h.projectBackups[projectID], b.ID)
	}
	h.Unlock()
	if b == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	h.RespondWithActivity(w, projectID, b.EnvironmentID, "environment.backup.delete")
}

func (h *Handler) handleRestoreBackup(w http.ResponseWriter, req *http.Request) {
	projectID := chi.URLParam(req, "project_id")
	var body map[string]any
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
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
		w.WriteHeader(http.StatusNotFound)
		return
	}
	h.RespondWithActivity(w, projectID, b.EnvironmentID, "environment.restore")
}

// BackupRestores returns the bodies of requests to restore a project's backups.
func (s *store) BackupRestores(projectID string) []map[string]any {
	s.RLock()
	defer s.RUnlock()
	return slices.Clone(s.backupRestores[projectID])
}

// ProjectBackupIDs returns the IDs of a project's backups.
func (s *store) ProjectBackupIDs(projectID string) []string {
	s.RLock()
	defer s.RUnlock()
	return slices.Sorted(maps.Keys(s.projectBackups[projectID]))
}
