package mockapi

// routeProjectResources adds routes to manage project resources such as backups, domains, integrations and settings.
func (h *Handler) routeProjectResources() {
	backupPath := "/projects/{project_id}/environments/{environment_id}/backups/{backup_id}"
	h.Get(backupPath, h.handleGetBackup)
	h.Delete(backupPath, h.handleDeleteBackup)
	h.Post(backupPath+"/restore", h.handleRestoreBackup)

	h.Post("/projects/{project_id}/domains", h.handleCreateDomain)
	h.Patch("/projects/{project_id}/domains/{name}", h.handlePatchDomain)
	h.Delete("/projects/{project_id}/domains/{name}", h.handleDeleteDomain)
	envDomainsPath := "/projects/{project_id}/environments/{environment_id}/domains"
	h.Get(envDomainsPath, h.handleListProjectDomains)
	h.Post(envDomainsPath, h.handleCreateDomain)
	h.Get(envDomainsPath+"/{name}", h.handleGetProjectDomain)
	h.Patch(envDomainsPath+"/{name}", h.handlePatchDomain)
	h.Delete(envDomainsPath+"/{name}", h.handleDeleteDomain)

	h.Post("/projects/{project_id}/certificates", h.handleCreateProjectCertificate)
	h.Delete("/projects/{project_id}/certificates/{certificate_id}", h.handleDeleteProjectCertificate)

	h.Post("/projects/{project_id}/integrations", h.handleCreateProjectIntegration)
	h.Patch("/projects/{project_id}/integrations/{integration_id}", h.handlePatchProjectIntegration)
	h.Delete("/projects/{project_id}/integrations/{integration_id}", h.handleDeleteProjectIntegration)
	h.Post("/projects/{project_id}/integrations/{integration_id}/validate", h.handleValidateProjectIntegration)

	h.Patch("/projects/{project_id}/settings", h.handlePatchProjectSettings)
	h.Patch("/projects/{project_id}/environments/{environment_id}/autoscaling", h.handlePatchAutoscalingSettings)
}
