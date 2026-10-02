package mockapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) handleGetBillingProfile(w http.ResponseWriter, req *http.Request) {
	h.RLock()
	defer h.RUnlock()
	p, ok := h.billingProfiles[chi.URLParam(req, "billing_profile_id")]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(p)
}

// handlePatchBillingProfile updates a billing profile. Like the real API, it
// requires the "update" link, and the "update-address" link for address fields.
func (h *Handler) handlePatchBillingProfile(w http.ResponseWriter, req *http.Request) {
	h.Lock()
	defer h.Unlock()
	p, ok := h.billingProfiles[chi.URLParam(req, "billing_profile_id")]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	var fields map[string]any
	if err := json.NewDecoder(req.Body).Decode(&fields); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if _, ok := p.Links["update"]; !ok {
		writeProblem(w, http.StatusForbidden, errors.New("forbidden"))
		return
	}
	for _, f := range []string{"billing_street_1", "billing_street_2", "locality", "administrative_area", "postal_code"} {
		if _, ok := fields[f]; !ok {
			continue
		}
		if _, ok := p.Links["update-address"]; !ok {
			writeProblem(w, http.StatusForbidden, errors.New("billing address cannot be edited for contracted customers"))
			return
		}
	}
	patched := *p
	b, _ := json.Marshal(fields)
	if err := json.Unmarshal(b, &patched); err != nil {
		writeProblem(w, http.StatusBadRequest, err)
		return
	}
	patched.Links = p.Links
	h.billingProfiles[p.ID] = &patched

	// The response does not include links.
	resp := patched
	resp.Links = nil
	_ = json.NewEncoder(w).Encode(resp)
}

func writeProblem(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": status,
		"title":  http.StatusText(status),
		"detail": err.Error(),
	})
}
