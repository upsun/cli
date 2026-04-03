// Package mockapi provides mocks of the HTTP API for use in integration tests.
package mockapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/require"
)

var TestPhoneVerificationCode = "123456"

type Handler struct {
	*chi.Mux

	t *testing.T

	store
}

func NewHandler(t *testing.T) *Handler {
	h := &Handler{t: t}
	h.Mux = chi.NewRouter()

	if testing.Verbose() {
		h.Use(middleware.DefaultLogger)
	}

	h.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			authHeader := req.Header.Get("Authorization")
			require.NotEmpty(t, authHeader)
			require.True(t, strings.HasPrefix(authHeader, "Bearer "))
			next.ServeHTTP(w, req)
		})
	})

	h.Get("/users/me", h.handleUsersMe)
	h.Get("/users/{user_id}/extended-access", h.handleUserExtendedAccess)
	h.Get("/ref/users", h.handleUserRefs)
	h.Post("/me/verification", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"state": false, "type": ""})
	})

	var (
		phoneVerifyMu      sync.Mutex
		pendingPhoneCode   = TestPhoneVerificationCode
		phoneVerifyPending bool
	)

	h.Post("/users/me/phone-verification", func(w http.ResponseWriter, req *http.Request) {
		phoneVerifyMu.Lock()
		phoneVerifyPending = true
		phoneVerifyMu.Unlock()
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
	})

	h.Post("/users/me/phone-verification/verify", func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(req.Body).Decode(&body)
		phoneVerifyMu.Lock()
		pending := phoneVerifyPending
		phoneVerifyMu.Unlock()
		if !pending || body.Code != pendingPhoneCode {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid code"})
			return
		}
		phoneVerifyMu.Lock()
		phoneVerifyPending = false
		phoneVerifyMu.Unlock()
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "verified"})
	})

	// PHP CLI-compatible phone verification endpoints.
	// The legacy PHP CLI calls /users/{id}/phonenumber to initiate and /users/{id}/phonenumber/{sid} to verify.
	const phoneSID = "test-sid-1"
	h.Post("/users/{user_id}/phonenumber", func(w http.ResponseWriter, req *http.Request) {
		phoneVerifyMu.Lock()
		phoneVerifyPending = true
		phoneVerifyMu.Unlock()
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"sid": phoneSID})
	})
	h.Post("/users/{user_id}/phonenumber/{sid}", func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(req.Body).Decode(&body)
		phoneVerifyMu.Lock()
		pending := phoneVerifyPending
		phoneVerifyMu.Unlock()
		if !pending || body.Code != pendingPhoneCode {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid code"})
			return
		}
		phoneVerifyMu.Lock()
		phoneVerifyPending = false
		phoneVerifyMu.Unlock()
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "verified"})
	})

	h.Get("/organizations", h.handleListOrgs)
	h.Post("/organizations", h.handleCreateOrg)
	h.Get("/organizations/{organization_id}", h.handleGetOrg)
	h.Patch("/organizations/{organization_id}", h.handlePatchOrg)
	h.Get("/users/{user_id}/organizations", h.handleListOrgs)
	h.Get("/ref/organizations", h.handleOrgRefs)

	h.Post("/organizations/{organization_id}/subscriptions", h.handleCreateSubscription)
	h.Get("/subscriptions/{subscription_id}", h.handleGetSubscription)
	h.Get("/organizations/{organization_id}/subscriptions/{subscription_id}", h.handleGetSubscription)
	h.Get("/organizations/{organization_id}/subscriptions/can-create", h.handleCanCreateSubscriptions)
	h.Get("/organizations/{organization_id}/setup/options", func(w http.ResponseWriter, _ *http.Request) {
		type options struct {
			Plans   []string `json:"plans"`
			Regions []string `json:"regions"`
		}
		_ = json.NewEncoder(w).Encode(options{[]string{"development"}, []string{"test-region"}})
	})
	h.Get("/organizations/{organization_id}/subscriptions/estimate", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"total": "$1,000 USD"})
	})

	h.Get("/projects/{project_id}", h.handleGetProject)
	h.Patch("/projects/{project_id}", h.handlePatchProject)
	h.Get("/projects/{project_id}/environments", h.handleListEnvironments)
	h.Get("/projects/{project_id}/environments/{environment_id}", h.handleGetEnvironment)
	h.Patch("/projects/{project_id}/environments/{environment_id}", h.handlePatchEnvironment)
	h.Get("/projects/{project_id}/environments/{environment_id}/settings", h.handleGetEnvironmentSettings)
	h.Patch("/projects/{project_id}/environments/{environment_id}/settings", h.handleSetEnvironmentSettings)
	h.Post("/projects/{project_id}/environments/{environment_id}/deploy", h.handleDeployEnvironment)
	h.Get("/projects/{project_id}/environments/{environment_id}/backups", h.handleListBackups)
	h.Post("/projects/{project_id}/environments/{environment_id}/backups", h.handleCreateBackup)
	h.Get("/projects/{project_id}/environments/{environment_id}/deployments/current", h.handleGetCurrentDeployment)
	h.Get("/projects/{project_id}/user-access", h.handleProjectUserAccess)
	h.Get("/ref/projects", h.handleProjectRefs)

	h.Get("/regions", h.handleListRegions)

	h.Get("/projects/{project_id}/activities", h.handleListProjectActivities)
	h.Get("/projects/{project_id}/activities/{id}", h.handleGetProjectActivity)
	h.Get("/projects/{project_id}/environments/{environment_id}/activities", h.handleListEnvironmentActivities)
	h.Get("/projects/{project_id}/environments/{environment_id}/activities/{id}", h.handleGetEnvironmentActivity)

	h.Get("/projects/{project_id}/variables", h.handleListProjectVariables)
	h.Post("/projects/{project_id}/variables", h.handleCreateProjectVariable)
	h.Get("/projects/{project_id}/variables/{name}", h.handleGetProjectVariable)
	h.Patch("/projects/{project_id}/variables/{name}", h.handlePatchProjectVariable)
	h.Get("/projects/{project_id}/environments/{environment_id}/variables", h.handleListEnvLevelVariables)
	h.Post("/projects/{project_id}/environments/{environment_id}/variables", h.handleCreateEnvLevelVariable)
	h.Get("/projects/{project_id}/environments/{environment_id}/variables/{name}", h.handleGetEnvLevelVariable)
	h.Patch("/projects/{project_id}/environments/{environment_id}/variables/{name}", h.handlePatchEnvLevelVariable)

	return h
}
