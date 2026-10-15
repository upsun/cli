package mockapi

// routeProjectResources adds routes to manage project resources such as backups, domains and integrations.
func (h *Handler) routeProjectResources() {
	backupPath := "/projects/{project_id}/environments/{environment_id}/backups/{backup_id}"
	h.Get(backupPath, h.handleGetBackup)
	h.Delete(backupPath, h.handleDeleteBackup)
	h.Post(backupPath+"/restore", h.handleRestoreBackup)
}
