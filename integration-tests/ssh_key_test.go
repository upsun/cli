package tests

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/upsun/cli/pkg/mockapi"
)

// TestSSHKey adds, lists and deletes SSH keys, with the legacy Accounts API and with the Auth API.
func TestSSHKey(t *testing.T) {
	t.Parallel()
	for _, source := range []string{mockapi.SSHKeySourceAccounts, mockapi.SSHKeySourceAuth} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			authServer := mockapi.NewAuthServer(t)
			defer authServer.Close()
			apiHandler := mockapi.NewHandler(t)
			if source == mockapi.SSHKeySourceAuth {
				apiHandler.SetSSHKeySource(source)
			}
			apiServer := httptest.NewServer(apiHandler)
			defer apiServer.Close()

			f := newCommandFactory(t, apiServer.URL, authServer.URL)
			publicKey, publicKeyPath := writeSSHKey(t, filepath.Join(f.home, ".ssh"), "id_ed25519")

			_, stdErr, err := f.RunCombinedOutput("ssh-keys")
			assertExitCode(t, 1, err)
			assert.Contains(t, stdErr, "You do not yet have any SSH public keys in your Platform.sh Testing account.")

			// The command asks for confirmation, recommending certificates instead.
			_, stdErr, err = f.RunCombinedOutput("ssh-key:add", publicKeyPath)
			assertExitCode(t, 1, err)
			assert.Contains(t, stdErr, "To load or check your SSH certificate, run: platform-test ssh-cert:load")
			assert.Empty(t, apiHandler.SSHKeys())

			_, stdErr, err = f.RunInteractive("y\n", "ssh-key:add", publicKeyPath, "--name", "Test key")
			require.NoError(t, err)
			assert.Contains(t, stdErr, "Adding an SSH key to your Platform.sh Testing account (my-user@example.com)")
			assert.Contains(t, stdErr, "The SSH key id_ed25519.pub has been successfully added")
			keys := apiHandler.SSHKeys()
			require.Len(t, keys, 1)
			assert.Equal(t, "Test key", keys[0].Label)
			assert.Equal(t, publicKey, keys[0].Value)
			id := keys[0].ID

			_, stdErr, err = f.RunInteractive("y\n", "ssh-key:add", publicKeyPath)
			require.NoError(t, err)
			assert.Contains(t, stdErr, "This key already exists in your account.")
			assert.Len(t, apiHandler.SSHKeys(), 1)

			// The local path is found by the key's fingerprint. The home directory is resolved (e.g. on macOS
			// /var is a symlink), so only the end of the path is compared.
			output := strings.TrimSpace(f.Run("ssh-keys", "--format", "csv"))
			assert.True(t, strings.HasPrefix(output, "ID,Label,Active,Local path\n"+id+",Test key,Yes,"), output)
			assert.True(t, strings.HasSuffix(output, filepath.Join(".ssh", "id_ed25519.pub")), output)
			assertTrimmed(t, mockapi.SSHKeyFingerprint(publicKey),
				f.Run("ssh-keys", "--format", "plain", "--columns", "sha256", "--no-header"))

			_, stdErr, err = f.RunCombinedOutput("ssh-key:delete", "999")
			assertExitCode(t, 1, err)
			assert.Contains(t, stdErr, "SSH key not found: 999")

			_, stdErr, err = f.RunCombinedOutput("ssh-key:delete")
			assertExitCode(t, 1, err)
			assert.Contains(t, stdErr, "You must specify the ID of the SSH key to delete.")

			_, stdErr, err = f.RunCombinedOutput("ssh-key:delete", id)
			require.NoError(t, err)
			assert.Contains(t, stdErr, "The SSH key "+id+" has been deleted from your Platform.sh Testing account.")
			assert.Empty(t, apiHandler.SSHKeys())
		})
	}
}

// TestSSHKeyAddInactive checks that adding a key that is in the account, but inactive, fails.
func TestSSHKeyAddInactive(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiHandler := mockapi.NewHandler(t)
	apiHandler.SetSSHKeySource(mockapi.SSHKeySourceAuth)
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	publicKey, publicKeyPath := writeSSHKey(t, t.TempDir(), "key")
	apiHandler.SetSSHKeys([]*mockapi.SSHKey{{ID: "inactive-key", Label: "Old key", Value: publicKey}})

	assertTrimmed(t, "inactive-key,Old key,No", f.Run("ssh-keys", "--format", "csv", "--no-header",
		"--columns", "id,label,active"))

	_, stdErr, err := f.RunInteractive("y\n", "ssh-key:add", publicKeyPath)
	assertExitCode(t, 1, err)
	assert.Contains(t, stdErr, "This key already exists in your account, but it is inactive.")
	assert.Len(t, apiHandler.SSHKeys(), 1)
}

// writeSSHKey writes a new ed25519 key pair, and returns the public key and its path.
func writeSSHKey(t *testing.T, dir, name string) (publicKey, publicKeyPath string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	sshPub, err := ssh.NewPublicKey(pub)
	require.NoError(t, err)
	block, err := ssh.MarshalPrivateKey(priv, "")
	require.NoError(t, err)

	require.NoError(t, os.MkdirAll(dir, 0o700))
	privateKeyPath := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(privateKeyPath, pem.EncodeToMemory(block), 0o600))
	publicKey = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))) + " test@example.com\n"
	require.NoError(t, os.WriteFile(privateKeyPath+".pub", []byte(publicKey), 0o600))
	return publicKey, privateKeyPath + ".pub"
}

// TestSSHKeyAddPaths checks how ssh-key:add finds or creates the public key.
func TestSSHKeyAddPaths(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiHandler := mockapi.NewHandler(t)
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	sshDir := filepath.Join(f.home, ".ssh")

	t.Run("errors", func(t *testing.T) {
		_, stdErr, err := f.RunInteractive("y\n", "ssh-key:add", filepath.Join(sshDir, "missing.pub"))
		assertExitCode(t, 1, err)
		assert.Contains(t, stdErr, "File not found: "+filepath.Join(sshDir, "missing.pub"))

		// The key is registered by another user.
		publicKey, publicKeyPath := writeSSHKey(t, t.TempDir(), "other")
		apiHandler.SetOtherUserSSHKeys([]string{publicKey})
		_, stdErr, err = f.RunInteractive("y\n", "ssh-key:add", publicKeyPath)
		assertExitCode(t, 1, err)
		assert.Contains(t, stdErr, "This SSH key is already registered.")
		assert.Empty(t, apiHandler.SSHKeys())
	})

	t.Run("private key path", func(t *testing.T) {
		publicKey, publicKeyPath := writeSSHKey(t, t.TempDir(), "key")
		_, stdErr, err := f.RunInteractive("y\n", "ssh-key:add", strings.TrimSuffix(publicKeyPath, ".pub"))
		require.NoError(t, err)
		assert.Contains(t, stdErr, "The SSH key key.pub has been successfully added")
		assert.True(t, slices.ContainsFunc(apiHandler.SSHKeys(), func(k *mockapi.SSHKey) bool {
			return k.Value == publicKey
		}))
	})

	t.Run("default key", func(t *testing.T) {
		// Without the default key, and declining to generate one, the path is required.
		_, stdErr, err := f.RunInteractive("y\nn\n", "ssh-key:add")
		assertExitCode(t, 1, err)
		assert.Contains(t, stdErr, "You must specify the path to a public SSH key")

		publicKey, _ := writeSSHKey(t, sshDir, "id_ed25519")
		_, stdErr, err = f.RunInteractive("y\ny\n", "ssh-key:add")
		require.NoError(t, err)
		assert.Contains(t, stdErr, "Use existing local key id_ed25519.pub? [Y/n]")
		assert.Contains(t, stdErr, "The SSH key id_ed25519.pub has been successfully added")
		assert.True(t, slices.ContainsFunc(apiHandler.SSHKeys(), func(k *mockapi.SSHKey) bool {
			return k.Value == publicKey
		}))
	})
}

// TestSSHKeyAddGenerate checks that ssh-key:add generates a key.
func TestSSHKeyAddGenerate(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen is not available")
	}
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiHandler := mockapi.NewHandler(t)
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)
	sshDir := filepath.Join(f.home, ".ssh")

	invalidPath := filepath.Join(t.TempDir(), "invalid.pub")
	require.NoError(t, os.WriteFile(invalidPath, []byte("not a key\n"), 0o600))
	_, stdErr, err := f.RunInteractive("y\n", "ssh-key:add", invalidPath)
	assertExitCode(t, 1, err)
	assert.Contains(t, stdErr, "The file does not contain a valid public key: "+invalidPath)

	// An existing file is not overwritten.
	require.NoError(t, os.MkdirAll(sshDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(sshDir, "taken"), nil, 0o600))
	_, stdErr, err = f.RunInteractive("y\ny\ntaken\nnew-key\n", "ssh-key:add")
	require.NoError(t, err)
	assert.Contains(t, stdErr, "Enter a filename for the new key (relative to ~/.ssh)")
	assert.Contains(t, stdErr, "The file already exists: "+filepath.Join(sshDir, "taken"))
	newKeyPath := filepath.Join(sshDir, "new-key")
	assert.Contains(t, stdErr, "Generated a new key: "+newKeyPath+".pub")
	assert.Contains(t, stdErr, "Add this key to an SSH agent with:\n    eval $(ssh-agent)\n    ssh-add ")
	publicKey, err := os.ReadFile(newKeyPath + ".pub")
	require.NoError(t, err)
	keys := apiHandler.SSHKeys()
	require.Len(t, keys, 1)
	assert.Equal(t, string(publicKey), keys[0].Value)
}

func TestSSHKeyDeleteInteractive(t *testing.T) {
	t.Parallel()
	authServer := mockapi.NewAuthServer(t)
	defer authServer.Close()
	apiHandler := mockapi.NewHandler(t)
	apiServer := httptest.NewServer(apiHandler)
	defer apiServer.Close()

	f := newCommandFactory(t, apiServer.URL, authServer.URL)

	_, stdErr, err := f.RunInteractive("", "ssh-key:delete")
	assertExitCode(t, 1, err)
	assert.Contains(t, stdErr, "You do not have any SSH keys in your account.")

	publicKey, _ := writeSSHKey(t, t.TempDir(), "key")
	apiHandler.SetSSHKeys([]*mockapi.SSHKey{
		{ID: "1", Label: "Key 1", Value: publicKey, Active: true},
		{ID: "2", Value: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGmJ6WD7HzRPI3IlHKjTSGa1sW6wVL9C0dZ3rbFx0qyF",
			Active: true},
	})
	_, stdErr, err = f.RunInteractive("1\ny\n", "ssh-key:delete")
	require.NoError(t, err)
	assert.Contains(t, stdErr, "Enter a number to choose a key to delete:")
	assert.Contains(t, stdErr, "[0] 1 (Key 1)")
	assert.Contains(t, stdErr, "The SSH key 2 has been deleted")
	keys := apiHandler.SSHKeys()
	require.Len(t, keys, 1)
	assert.Equal(t, "1", keys[0].ID)
}
