package tests

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

func TestCertificateList(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	projectID := mockapi.ProjectID()
	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments",
		),
		DefaultBranch: "main",
	}})

	main := makeEnv(projectID, "main", "production", "active", nil)
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	created, _ := time.Parse(time.RFC3339, "2024-01-01T00:00:00Z")
	expires, _ := time.Parse(time.RFC3339, "2027-01-01T00:00:00Z")
	apiHandler.SetProjectCertificates(projectID, []*mockapi.Certificate{
		{
			ID:            "cert1",
			Domains:       []string{"example.com", "www.example.com"},
			Issuer:        "Custom CA",
			IsProvisioned: false,
			ExpiresAt:     expires,
			CreatedAt:     created,
			UpdatedAt:     created,
			Links: mockapi.MakeHALLinks(
				"self=/projects/" + projectID + "/certificates/cert1",
			),
		},
	})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.Run("cc")

	// List certificates.
	output := f.Run("certificates", "-p", projectID)
	assert.Contains(t, output, "cert1")
	assert.Contains(t, output, "example.com")

	// Get a specific certificate property.
	assertTrimmed(t, "Custom CA", f.Run("certificate:get", "-p", projectID, "cert1", "-P", "issuer"))
}

func TestCertificateListEmpty(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	projectID := mockapi.ProjectID()
	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments",
		),
		DefaultBranch: "main",
	}})

	main := makeEnv(projectID, "main", "production", "active", nil)
	apiHandler.SetEnvironments([]*mockapi.Environment{main})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	f.Run("cc")

	// Empty certificate list.
	_, stdErr, err := f.RunCombinedOutput("certificates", "-p", projectID)
	assert.NoError(t, err)
	assert.Contains(t, stdErr, "No certificates found")
}

// writeSelfSignedCert writes a self-signed certificate for the domains, and its private key, to temporary files.
func writeSelfSignedCert(t *testing.T, domains ...string) (certPath, keyPath string) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: domains[0]},
		DNSNames:     domains,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)

	dir := t.TempDir()
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")
	require.NoError(t, os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	require.NoError(t, os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600))
	return certPath, keyPath
}

func TestCertificateAddDelete(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()

	apiHandler := mockapi.NewHandler(t)
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	projectID := mockapi.ProjectID()
	apiHandler.SetProjects([]*mockapi.Project{{
		ID: projectID,
		Links: mockapi.MakeHALLinks(
			"self=/projects/"+projectID,
			"environments=/projects/"+projectID+"/environments",
		),
		DefaultBranch: "main",
	}})
	apiHandler.SetEnvironments([]*mockapi.Environment{makeEnv(projectID, "main", "production", "active", nil)})
	apiHandler.SetProjectCertificates(projectID, []*mockapi.Certificate{{
		ID:            "provisioned1",
		Domains:       []string{"main.example.com"},
		IsProvisioned: true,
		Links:         mockapi.MakeHALLinks("self=/projects/" + projectID + "/certificates/provisioned1"),
	}})

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	certPath, keyPath := writeSelfSignedCert(t, "example.com", "www.example.com")

	_, stderr, err := f.RunCombinedOutput("certificate:add", "-p", projectID, "--cert", certPath)
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "The --cert and --key options are required")

	_, otherKeyPath := writeSelfSignedCert(t, "other.example.com")
	_, stderr, err = f.RunCombinedOutput("certificate:add", "-p", projectID, "--cert", certPath, "--key", otherKeyPath)
	assert.Error(t, err)
	assert.Contains(t, stderr, "The provided certificate does not match the provided private key.")

	_, stderr, err = f.RunCombinedOutput("certificate:add", "-p", projectID, "--cert", certPath, "--key", keyPath)
	require.NoError(t, err, stderr)
	assert.Contains(t, stderr, "The activity succeeded")

	ids := apiHandler.ProjectCertificateIDs(projectID)
	require.Len(t, ids, 2)
	newID := ids[1]
	assertTrimmed(t, "- example.com\n- www.example.com",
		f.Run("certificate:get", "-p", projectID, newID, "-P", "domains"))

	_, stderr, err = f.RunCombinedOutput("certificate:delete", "-p", projectID, "provisioned1")
	assertExitCode(t, 1, err)
	assert.Contains(t, stderr, "The certificate provisioned1 is automatically provisioned; it cannot be deleted.")

	// Delete by a partial ID.
	_, stderr, err = f.RunCombinedOutput("certificate:delete", "-p", projectID, newID[:8])
	require.NoError(t, err, stderr)
	assert.Contains(t, stderr, "The certificate "+newID+" has been deleted.")
	assert.Equal(t, []string{"provisioned1"}, apiHandler.ProjectCertificateIDs(projectID))
}
