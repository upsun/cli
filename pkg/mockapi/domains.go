package mockapi

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// domainStore returns the map and key for the domains in the request. Like the
// API, a project's domains are the production domains, which are also the
// default branch's environment domains. The caller must hold the write lock.
func (h *Handler) domainStore(req *http.Request) (domains map[string][]*Domain, key string, production bool) {
	projectID := chi.URLParam(req, "project_id")
	environmentID := chi.URLParam(req, "environment_id")
	if p, ok := h.projects[projectID]; environmentID == "" || (ok && environmentID == p.DefaultBranch) {
		if h.projectDomains == nil {
			h.projectDomains = make(map[string][]*Domain)
		}
		return h.projectDomains, projectID, true
	}
	if h.environmentDomains == nil {
		h.environmentDomains = make(map[string][]*Domain)
	}
	return h.environmentDomains, projectID + "/" + environmentID, false
}

// domainCollectionPath returns the path of the collection of domains in the request.
func domainCollectionPath(req *http.Request) string {
	path := "/projects/" + url.PathEscape(chi.URLParam(req, "project_id"))
	if environmentID := chi.URLParam(req, "environment_id"); environmentID != "" {
		path += "/environments/" + url.PathEscape(environmentID)
	}
	return path + "/domains"
}

func copyDomain(d *Domain) *Domain {
	c := *d
	c.SSL = nil
	c.Links = maps.Clone(d.Links)
	return &c
}

// domainView returns a copy of a domain as the API shows it, with links to view,
// edit and delete it unless it has links, and SSL information from the project's
// certificates. The caller must hold a lock.
func (h *Handler) domainView(req *http.Request, d *Domain) *Domain {
	c := copyDomain(d)
	if c.Links == nil {
		path := domainCollectionPath(req) + "/" + url.PathEscape(d.Name)
		c.Links = MakeHALLinks("self="+path, "#edit="+path, "#delete="+path)
	}
	c.SSL = &DomainSSL{}
	for _, cert := range h.projectCertificates[chi.URLParam(req, "project_id")] {
		if slices.Contains(cert.Domains, d.Name) {
			expires := cert.ExpiresAt
			c.SSL = &DomainSSL{HasCertificate: true, Certificate: cert.Certificate, ExpiresOn: &expires}
			break
		}
	}
	return c
}

func domainName(req *http.Request) string {
	name, _ := url.PathUnescape(chi.URLParam(req, "name"))
	return name
}

// domainActivity returns an activity for an operation on a domain. Domain
// activities belong to an environment, even for project domains.
func (h *Handler) domainActivity(req *http.Request, op string) *Activity {
	projectID := chi.URLParam(req, "project_id")
	environmentID := chi.URLParam(req, "environment_id")
	if environmentID == "" {
		h.RLock()
		if p, ok := h.projects[projectID]; ok {
			environmentID = p.DefaultBranch
		}
		h.RUnlock()
	}
	return &Activity{Type: "environment.domain." + op, Project: projectID, Environments: []string{environmentID}}
}

func (h *Handler) handleListProjectDomains(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	domains, key, _ := h.domainStore(req)
	result := make([]*Domain, 0, len(domains[key]))
	for _, d := range domains[key] {
		result = append(result, h.domainView(req, d))
	}
	_ = json.NewEncoder(w).Encode(result)
}

func (h *Handler) handleGetProjectDomain(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	domains, key, _ := h.domainStore(req)
	name := domainName(req)
	for _, d := range domains[key] {
		if d.Name == name {
			_ = json.NewEncoder(w).Encode(h.domainView(req, d))
			return
		}
	}
	writeError(w, http.StatusNotFound, "Not found", nil)
}

// domainInput is the body of a request to add or update a domain.
type domainInput struct {
	Name           string            `json:"name"`
	ReplacementFor string            `json:"replacement_for"`
	IsDefault      *bool             `json:"is_default"`
	SSL            *certificateInput `json:"ssl"`
}

// handleCreateDomain adds a production domain, or a replacement domain on a non-production environment.
func (h *Handler) handleCreateDomain(w http.ResponseWriter, req *http.Request) {
	projectID := chi.URLParam(req, "project_id")
	if environmentID := chi.URLParam(req, "environment_id"); environmentID != "" &&
		h.findEnvironment(projectID, environmentID) == nil {
		writeError(w, http.StatusNotFound, "Not found", nil)
		return
	}
	var input domainInput
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil || input.Name == "" {
		writeError(w, http.StatusBadRequest, "Invalid domain", map[string]any{"name": "Required"})
		return
	}
	activity := h.domainActivity(req, "create")

	h.Lock()
	domains, key, production := h.domainStore(req)
	code, message, detail := h.validateNewDomain(domains[key], projectID, production, input)
	if code == 0 && input.SSL != nil {
		// Like the API, a certificate is added for the domain, unless it exists.
		if _, c, m := h.createCertificate(projectID, *input.SSL); c == http.StatusBadRequest {
			code, message = c, m
		}
	}
	var entity *Domain
	if code == 0 {
		now := time.Now()
		d := &Domain{
			ID:             input.Name,
			Name:           input.Name,
			Type:           "replacement",
			ReplacementFor: input.ReplacementFor,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if production {
			d.Type = "production"
			d.IsDefault = input.IsDefault != nil && *input.IsDefault
		}
		domains[key] = append(domains[key], d)
		entity = h.domainView(req, d)
	}
	h.Unlock()
	if code != 0 {
		writeError(w, code, message, detail)
		return
	}
	h.respond(w, "created", http.StatusCreated, entity, activity)
}

// validateNewDomain checks a new domain as the API does, and returns an error
// status, message and details, or a zero status. The caller must hold a lock.
func (h *Handler) validateNewDomain(
	existing []*Domain, projectID string, production bool, input domainInput,
) (code int, message string, detail map[string]any) {
	if slices.ContainsFunc(existing, func(d *Domain) bool { return d.Name == input.Name }) {
		return http.StatusConflict, "The domain already exists", nil
	}
	if production {
		return 0, "", nil
	}
	if input.ReplacementFor == "" {
		message = "The replacement_for is required for non-production environments' domains."
		return http.StatusBadRequest, message, map[string]any{"replacement_for": message}
	}
	prodDomains := make([]string, 0, len(h.projectDomains[projectID]))
	for _, d := range h.projectDomains[projectID] {
		prodDomains = append(prodDomains, d.Name)
	}
	if !slices.Contains(prodDomains, input.ReplacementFor) {
		return http.StatusConflict,
			"The replacement_for has no corresponding domain set on the production environment.",
			map[string]any{"prod-domains": prodDomains}
	}
	for _, d := range existing {
		if d.ReplacementFor == input.ReplacementFor {
			return http.StatusConflict,
				"The environment already has a domain with the same replacement_for value",
				map[string]any{"conflicting_domain": d.Name}
		}
	}
	return 0, "", nil
}

// handlePatchDomain updates a domain. Only "ssl" and, for production domains, "is_default" can be changed.
func (h *Handler) handlePatchDomain(w http.ResponseWriter, req *http.Request) {
	projectID := chi.URLParam(req, "project_id")
	var input domainInput
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON", nil)
		return
	}
	activity := h.domainActivity(req, "update")

	h.Lock()
	domains, key, production := h.domainStore(req)
	name := domainName(req)
	i := slices.IndexFunc(domains[key], func(d *Domain) bool { return d.Name == name })
	code, message := 0, ""
	if i == -1 {
		code, message = http.StatusNotFound, "Not found"
	} else if input.SSL != nil {
		if _, c, m := h.createCertificate(projectID, *input.SSL); c == http.StatusBadRequest {
			code, message = c, m
		}
	}
	var entity *Domain
	if code == 0 {
		patched := copyDomain(domains[key][i])
		if production && input.IsDefault != nil {
			patched.IsDefault = *input.IsDefault
		}
		patched.UpdatedAt = time.Now()
		domains[key][i] = patched
		entity = h.domainView(req, patched)
	}
	h.Unlock()
	if code != 0 {
		writeError(w, code, message, nil)
		return
	}
	h.respond(w, "OK", http.StatusOK, entity, activity)
}

// handleDeleteDomain deletes a domain. Like the API, deleting a production
// domain also deletes the non-production domains that replace it.
func (h *Handler) handleDeleteDomain(w http.ResponseWriter, req *http.Request) {
	projectID := chi.URLParam(req, "project_id")
	activity := h.domainActivity(req, "delete")

	h.Lock()
	domains, key, production := h.domainStore(req)
	name := domainName(req)
	n := len(domains[key])
	domains[key] = slices.DeleteFunc(domains[key], func(d *Domain) bool { return d.Name == name })
	deleted := len(domains[key]) < n
	if deleted && production {
		for k, envDomains := range h.environmentDomains {
			if strings.HasPrefix(k, projectID+"/") {
				h.environmentDomains[k] = slices.DeleteFunc(envDomains, func(d *Domain) bool {
					return d.ReplacementFor == name
				})
			}
		}
	}
	h.Unlock()
	if !deleted {
		writeError(w, http.StatusNotFound, "Not found", nil)
		return
	}
	h.respond(w, "deleted", http.StatusOK, nil, activity)
}

// SetEnvironmentDomains sets the domains of a non-production environment.
func (s *store) SetEnvironmentDomains(projectID, environmentID string, domains []*Domain) {
	s.Lock()
	defer s.Unlock()
	if s.environmentDomains == nil {
		s.environmentDomains = make(map[string][]*Domain)
	}
	s.environmentDomains[projectID+"/"+environmentID] = slices.Clone(domains)
}

// Domains returns copies of a project's domains, or of a non-production
// environment's if environmentID is not empty. SSL information is not included.
func (s *store) Domains(projectID, environmentID string) []*Domain {
	s.RLock()
	defer s.RUnlock()
	domains := s.projectDomains[projectID]
	if environmentID != "" {
		domains = s.environmentDomains[projectID+"/"+environmentID]
	}
	result := make([]*Domain, 0, len(domains))
	for _, d := range domains {
		result = append(result, copyDomain(d))
	}
	return result
}
