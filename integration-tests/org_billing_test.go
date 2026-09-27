package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

// setUpBillingOrg configures an org (named "acme") and returns a command factory.
func setUpBillingOrg(
	t *testing.T, org *mockapi.Org, profiles ...*mockapi.BillingProfile,
) (*mockapi.Handler, *cmdFactory) {
	authServer := mockapi.NewAuthServer(t)
	t.Cleanup(authServer.Close)

	myUserID := "my-user-id"
	org.Owner = myUserID
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetMyUser(&mockapi.User{ID: myUserID})
	apiHandler.SetOrgs([]*mockapi.Org{org})
	apiHandler.SetBillingProfiles(profiles)

	apiServer := httptest.NewServer(apiHandler)
	t.Cleanup(apiServer.Close)

	return apiHandler, newCommandFactory(t, apiServer.URL, authServer.URL)
}

// newBillingOrg returns an org on the new billing system.
func newBillingOrg(profileID string, withLink bool) *mockapi.Org {
	id := "org-new-billing"
	links := []string{"self=/organizations/" + url.PathEscape(id)}
	if withLink {
		links = append(links, "billing-profile=/billing/profiles/"+profileID)
	}
	return &mockapi.Org{
		ID:               id,
		Type:             "flexible",
		Name:             "acme",
		Label:            "Acme",
		Capabilities:     []string{},
		Links:            mockapi.MakeHALLinks(links...),
		BillingLegacy:    new(false),
		BillingProfileID: profileID,
	}
}

func newBillingProfile(links ...string) *mockapi.BillingProfile {
	profilePath := "/billing/profiles/bp-1"
	halLinks := make([]string, 0, 1+len(links))
	halLinks = append(halLinks, "self="+profilePath)
	for _, l := range links {
		halLinks = append(halLinks, l+"="+profilePath)
	}
	return &mockapi.BillingProfile{
		ID:              "bp-1",
		Name:            "Acme Ltd",
		BillingEmail:    "billing@example.com",
		BillingContacts: "a@example.com",
		Country:         "FR",
		Currency:        "EUR",
		BillingStreet1:  "1 Rue Example",
		Locality:        "Paris",
		PostalCode:      "75001",
		PricingModel:    "flex",
		Links:           mockapi.MakeHALLinks(halLinks...),
	}
}

func TestOrgBillingProfile_NewBilling(t *testing.T) {
	apiHandler, f := setUpBillingOrg(t, newBillingOrg("bp-1", true), newBillingProfile("update", "update-address"))

	out, _, err := f.RunCombinedOutput("org:billing:profile", "-o", "acme", "--format", "csv")
	require.NoError(t, err)
	assert.Equal(t, "Property,Value\nbilling_profile_id,bp-1\nname,Acme Ltd\nbilling_email,billing@example.com\n"+
		"billing_contacts,a@example.com\ncurrency,EUR\npricing_model,flex\n", out)

	assert.Equal(t, "billing@example.com\n", f.Run("org:billing:profile", "-o", "acme", "billing_email"))

	_, stdErr, err := f.RunCombinedOutput("org:billing:profile", "-o", "acme", "name", "Acme SAS")
	require.NoError(t, err)
	assert.Contains(t, stdErr, "Property name set to: Acme SAS")
	assert.Equal(t, "Acme SAS", apiHandler.BillingProfile("bp-1").Name)

	_, stdErr, err = f.RunCombinedOutput("org:billing:profile", "-o", "acme", "vat_number", "FR123")
	assert.Error(t, err)
	assert.Contains(t, stdErr, "Property not writable: vat_number")
}

func TestOrgBillingAddress_NewBilling(t *testing.T) {
	apiHandler, f := setUpBillingOrg(t, newBillingOrg("bp-1", true), newBillingProfile("update", "update-address"))

	out, _, err := f.RunCombinedOutput("org:billing:address", "-o", "acme", "--format", "csv")
	require.NoError(t, err)
	assert.Equal(t, "Property,Value\ncountry,FR\nbilling_street_1,1 Rue Example\nbilling_street_2,\n"+
		"locality,Paris\nadministrative_area,\npostal_code,75001\n", out)

	_, stdErr, err := f.RunCombinedOutput("org:billing:address", "-o", "acme",
		"billing_street_1", "2 Rue Example", "postal_code", "75002")
	require.NoError(t, err)
	assert.Contains(t, stdErr,
		`Updating the address with values: {"billing_street_1":"2 Rue Example","postal_code":"75002"}`)
	p := apiHandler.BillingProfile("bp-1")
	assert.Equal(t, "2 Rue Example", p.BillingStreet1)
	assert.Equal(t, "75002", p.PostalCode)

	// The API silently ignores country changes (except from staff).
	_, stdErr, err = f.RunCombinedOutput("org:billing:address", "-o", "acme", "country", "DE")
	assert.Error(t, err)
	assert.Contains(t, stdErr, "Property not writable: country")
	assert.Equal(t, "FR", apiHandler.BillingProfile("bp-1").Country)
}

func TestOrgBillingAddress_NewBillingValidationError(t *testing.T) {
	apiHandler, f := setUpBillingOrg(t, newBillingOrg("bp-1", true), newBillingProfile("update", "update-address"))
	apiHandler.Patch("/billing/profiles/bp-1", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": http.StatusBadRequest,
			"title":  "Bad Request",
			"detail": "Invalid address",
			"errors": map[string]any{"postal_code": "invalid postal code", "locality": []string{"required"}},
		})
	})

	_, stdErr, err := f.RunCombinedOutput("org:billing:address", "-o", "acme", "postal_code", "invalid")
	assert.Error(t, err)
	assert.Contains(t, stdErr, "Invalid address\n")
	assert.Contains(t, stdErr, "  postal_code: invalid postal code\n")
	assert.Contains(t, stdErr, "  locality: required\n")
}

func TestOrgBilling_NewBillingReadOnly(t *testing.T) {
	apiHandler, f := setUpBillingOrg(t, newBillingOrg("bp-1", true), newBillingProfile())

	_, stdErr, err := f.RunCombinedOutput("org:billing:profile", "-o", "acme", "name", "Acme SAS")
	assert.Error(t, err)
	assert.Contains(t, stdErr, "You do not have permission to update the billing profile.")

	_, stdErr, err = f.RunCombinedOutput("org:billing:address", "-o", "acme", "postal_code", "75002")
	assert.Error(t, err)
	assert.Contains(t, stdErr, "You do not have permission to update the billing address.")

	assert.Equal(t, newBillingProfile(), apiHandler.BillingProfile("bp-1"))
}

func TestOrgBilling_NewBillingNoAccess(t *testing.T) {
	cases := []struct {
		name      string
		profileID string
		expected  string
	}{
		{"no permission", "bp-1", "You do not have access to the billing profile of the organization"},
		{"no profile", "", "No billing profile is attached to the organization"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, f := setUpBillingOrg(t, newBillingOrg(c.profileID, false))
			for _, cmd := range []string{"org:billing:profile", "org:billing:address"} {
				_, stdErr, err := f.RunCombinedOutput(cmd, "-o", "acme")
				assert.Error(t, err)
				assert.Contains(t, stdErr, c.expected)
			}
		})
	}
}

func TestOrgBilling_Legacy(t *testing.T) {
	apiHandler, f := setUpBillingOrg(t, &mockapi.Org{
		ID:           "org-legacy",
		Type:         "flexible",
		Name:         "acme",
		Label:        "Acme",
		Capabilities: []string{},
		Links:        mockapi.MakeHALLinks("self=/organizations/org-legacy", "orders=/organizations/org-legacy/orders"),
	})
	apiHandler.Get("/organizations/org-legacy/profile", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":           "org-legacy",
			"company_name": "Acme Legacy",
			"_links":       mockapi.MakeHALLinks("self=/organizations/org-legacy/profile"),
		})
	})
	apiHandler.Get("/organizations/org-legacy/address", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"country":     "FR",
			"postal_code": "75001",
			"_links":      mockapi.MakeHALLinks("self=/organizations/org-legacy/address"),
		})
	})

	assert.Equal(t, "Acme Legacy\n", f.Run("org:billing:profile", "-o", "acme", "company_name"))
	assert.Equal(t, "75001\n", f.Run("org:billing:address", "-o", "acme", "postal_code"))
}
