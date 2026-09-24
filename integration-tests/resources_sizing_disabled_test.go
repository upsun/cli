package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestResourcesSizingDisabled checks the error shown when the project does not
// support flexible resources, which mentions Upsun Fixed only for Fixed orgs.
func TestResourcesSizingDisabled(t *testing.T) {
	const fixedNote = "not available on Upsun Fixed"

	commands := [][]string{
		{"resources:get", "-e", "main"},
		{"resources:set", "-e", "main", "--size", "app:0.5"},
		{"resources:size:list", "-e", "main"},
		{"resources:build:get"},
		{"resources:build:set", "--cpu", "1"},
	}
	cases := []struct {
		name      string
		orgType   string
		wantFixed bool
	}{
		{"fixed org", "fixed", true},
		{"flexible org", "flexible", false},
		{"inaccessible org", "", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			authServer := mockapi.NewAuthServer(t)
			defer authServer.Close()

			apiHandler := mockapi.NewHandler(t)

			myUserID := "my-user-id"
			orgID := "org-id"
			apiHandler.SetMyUser(&mockapi.User{ID: myUserID})
			if c.orgType != "" {
				apiHandler.SetOrgs([]*mockapi.Org{{
					ID:           orgID,
					Type:         c.orgType,
					Name:         "acme",
					Label:        "Acme",
					Owner:        myUserID,
					Capabilities: []string{},
					Links:        mockapi.MakeHALLinks("self=/organizations/" + url.PathEscape(orgID)),
				}})
			}

			projectID := mockapi.ProjectID()
			apiHandler.SetProjects([]*mockapi.Project{{
				ID:           projectID,
				Organization: orgID,
				Links: mockapi.MakeHALLinks(
					"self=/projects/"+projectID,
					"environments=/projects/"+projectID+"/environments",
				),
				DefaultBranch: "main",
			}})
			apiHandler.SetEnvironments([]*mockapi.Environment{
				makeEnv(projectID, "main", "production", "active", nil),
			})
			apiHandler.Get("/projects/"+projectID+"/settings", func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"sizing_api_enabled": false})
			})

			apiServer := httptest.NewServer(apiHandler)
			defer apiServer.Close()

			f := newCommandFactory(t, apiServer.URL, authServer.URL)

			for _, args := range commands {
				t.Run(args[0], func(t *testing.T) {
					_, stderr, err := f.RunCombinedOutput(append(args, "-p", projectID)...)
					require.Error(t, err)
					assert.Contains(t, stderr, "The flexible resources API is not enabled for the project")
					if c.wantFixed {
						assert.Contains(t, stderr, fixedNote)
						assert.Contains(t, stderr, "https://docs.upsun.com/anchors/fixed/")
					} else {
						assert.NotContains(t, strings.ToLower(stderr), "fixed")
					}
				})
			}
		})
	}
}
