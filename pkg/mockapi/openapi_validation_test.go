package mockapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/upsun/cli/pkg/mockapi"
)

func TestOpenAPIValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping OpenAPI validation in short mode")
	}

	// Enable OpenAPI validation for this test
	os.Setenv("VALIDATE_OPENAPI", "1")
	defer os.Unsetenv("VALIDATE_OPENAPI")

	handler := mockapi.NewHandler(t)

	// Setup some test data
	projectID := mockapi.ProjectID()
	orgID := "org-" + mockapi.NumericID()

	handler.SetMyUser(&mockapi.User{
		ID:        "user-123",
		Username:  "testuser",
		Email:     "test@example.com",
		FirstName: "Test",
		LastName:  "User",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	handler.SetOrgs([]*mockapi.Org{
		{
			ID:    orgID,
			Name:  "test-org",
			Label: "Test Organization",
			Owner: "user-123",
			Type:  "organization",
			Links: mockapi.MakeHALLinks(
				"self=/organizations/"+orgID,
			),
		},
	})

	handler.SetProjects([]*mockapi.Project{
		{
			ID:            projectID,
			Title:         "Test Project",
			Region:        "us-1",
			Organization:  orgID,
			DefaultBranch: "main",
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
			Links: mockapi.MakeHALLinks(
				"self=/projects/"+projectID,
			),
		},
	})

	handler.SetEnvironments([]*mockapi.Environment{
		{
			ID:          "main",
			Name:        "main",
			MachineName: "main",
			Title:       "Main",
			Type:        "production",
			Status:      "active",
			Project:     projectID,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
			Links: mockapi.MakeHALLinks(
				"self=/projects/"+projectID+"/environments/main",
			),
		},
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	// Test cases that should validate against OpenAPI spec
	testCases := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{
			name:       "get current user",
			method:     "GET",
			path:       "/users/me",
			wantStatus: http.StatusOK,
		},
		{
			name:       "list organizations",
			method:     "GET",
			path:       "/organizations",
			wantStatus: http.StatusOK,
		},
		{
			name:       "get organization",
			method:     "GET",
			path:       "/organizations/" + orgID,
			wantStatus: http.StatusOK,
		},
		{
			name:       "get project",
			method:     "GET",
			path:       "/projects/" + projectID,
			wantStatus: http.StatusOK,
		},
		{
			name:       "list environments",
			method:     "GET",
			path:       "/projects/" + projectID + "/environments",
			wantStatus: http.StatusOK,
		},
		{
			name:       "get environment",
			method:     "GET",
			path:       "/projects/" + projectID + "/environments/main",
			wantStatus: http.StatusOK,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, server.URL+tc.path, nil)
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer test-token")

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tc.wantStatus, resp.StatusCode)

			// Verify we got valid JSON
			var result map[string]interface{}
			err = json.NewDecoder(resp.Body).Decode(&result)
			require.NoError(t, err, "Response should be valid JSON")
		})
	}
}

// TestOpenAPIValidationDisabledByDefault ensures validation is opt-in
func TestOpenAPIValidationDisabledByDefault(t *testing.T) {
	// Ensure VALIDATE_OPENAPI is not set
	os.Unsetenv("VALIDATE_OPENAPI")

	handler := mockapi.NewHandler(t)

	// This should work without OpenAPI validation
	req := httptest.NewRequest("GET", "/users/me", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Should succeed even without OpenAPI spec validation
	assert.Equal(t, http.StatusOK, rec.Code)
}
