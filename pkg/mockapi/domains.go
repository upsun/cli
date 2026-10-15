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

// domainStore returns the map and key for the domains of the request's
// project, or of its environment if there is one. The caller must hold the write lock.
func (h *Handler) domainStore(req *http.Request) (domains map[string][]*Domain, key string) {
	projectID := chi.URLParam(req, "project_id")
	if environmentID := chi.URLParam(req, "environment_id"); environmentID != "" {
		if h.environmentDomains == nil {
			h.environmentDomains = make(map[string][]*Domain)
		}
		return h.environmentDomains, projectID + "/" + environmentID
	}
	if h.projectDomains == nil {
		h.projectDomains = make(map[string][]*Domain)
	}
	return h.projectDomains, projectID
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
	if c.SSL != nil {
		ssl := *c.SSL
		ssl.Chain = slices.Clone(ssl.Chain)
		c.SSL = &ssl
	}
	c.Links = maps.Clone(d.Links)
	return &c
}

// domainWithLinks returns a copy of a domain, with links to view, edit and delete it unless it has links.
func domainWithLinks(collectionPath string, d *Domain) *Domain {
	c := copyDomain(d)
	if c.Links == nil {
		path := collectionPath + "/" + url.PathEscape(d.Name)
		c.Links = MakeHALLinks("self="+path, "#edit="+path, "#delete="+path)
	}
	return c
}

func domainName(req *http.Request) string {
	name, _ := url.PathUnescape(chi.URLParam(req, "name"))
	return name
}

func (h *Handler) handleListProjectDomains(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	domains, key := h.domainStore(req)
	result := make([]*Domain, 0, len(domains[key]))
	for _, d := range domains[key] {
		result = append(result, domainWithLinks(domainCollectionPath(req), d))
	}
	_ = json.NewEncoder(w).Encode(result)
}

func (h *Handler) handleGetProjectDomain(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	domains, key := h.domainStore(req)
	name := domainName(req)
	for _, d := range domains[key] {
		if d.Name == name {
			_ = json.NewEncoder(w).Encode(domainWithLinks(domainCollectionPath(req), d))
			return
		}
	}
	w.WriteHeader(http.StatusNotFound)
}

func (h *Handler) handleCreateDomain(w http.ResponseWriter, req *http.Request) {
	projectID := chi.URLParam(req, "project_id")
	environmentID := chi.URLParam(req, "environment_id")
	domainType := "production"
	activityType := "project.domain.create"
	var environments []string
	if environmentID != "" {
		env := h.findEnvironment(projectID, environmentID)
		if env == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		domainType = env.Type
		activityType = "environment.domain.create"
		environments = []string{environmentID}
	}

	var d Domain
	if err := json.NewDecoder(req.Body).Decode(&d); err != nil || d.Name == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	now := time.Now()
	d.ID = d.Name
	d.Type = domainType
	d.CreatedAt = now
	d.UpdatedAt = now
	if d.SSL == nil {
		d.SSL = &DomainSSL{}
	}
	d.SSL.HasCertificate = d.SSL.Certificate != ""

	h.Lock()
	domains, key := h.domainStore(req)
	exists := slices.ContainsFunc(domains[key], func(e *Domain) bool { return e.Name == d.Name })
	if !exists {
		domains[key] = append(domains[key], &d)
	}
	entity := domainWithLinks(domainCollectionPath(req), &d)
	h.Unlock()
	if exists {
		w.WriteHeader(http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusCreated)
	h.respondWithEntity(w, entity, &Activity{Type: activityType, Project: projectID, Environments: environments})
}

func (h *Handler) handlePatchDomain(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	domains, key := h.domainStore(req)
	name := domainName(req)
	i := slices.IndexFunc(domains[key], func(d *Domain) bool { return d.Name == name })
	if i == -1 {
		h.Unlock()
		w.WriteHeader(http.StatusNotFound)
		return
	}
	patched := copyDomain(domains[key][i])
	if err := json.NewDecoder(req.Body).Decode(patched); err != nil {
		h.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if patched.SSL != nil {
		patched.SSL.HasCertificate = patched.SSL.Certificate != ""
	}
	patched.UpdatedAt = time.Now()
	domains[key][i] = patched
	entity := domainWithLinks(domainCollectionPath(req), patched)
	h.Unlock()
	h.respondWithEntity(w, entity, domainActivity(req, "update"))
}

func (h *Handler) handleDeleteDomain(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	domains, key := h.domainStore(req)
	name := domainName(req)
	n := len(domains[key])
	domains[key] = slices.DeleteFunc(domains[key], func(d *Domain) bool { return d.Name == name })
	deleted := len(domains[key]) < n
	h.Unlock()
	if !deleted {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	h.respondWithActivity(w, domainActivity(req, "delete"))
}

// domainActivity returns an activity for an operation on a project or environment domain.
func domainActivity(req *http.Request, op string) *Activity {
	a := &Activity{Type: "project.domain." + op, Project: chi.URLParam(req, "project_id")}
	if environmentID := chi.URLParam(req, "environment_id"); environmentID != "" {
		a.Type = strings.Replace(a.Type, "project.", "environment.", 1)
		a.Environments = []string{environmentID}
	}
	return a
}

// SetEnvironmentDomains sets the domains of a non-production environment.
func (s *store) SetEnvironmentDomains(projectID, environmentID string, domains []*Domain) {
	s.Lock()
	defer s.Unlock()
	if s.environmentDomains == nil {
		s.environmentDomains = make(map[string][]*Domain)
	}
	s.environmentDomains[projectID+"/"+environmentID] = domains
}

// Domains returns copies of a project's domains, or of an environment's if environmentID is not empty.
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
