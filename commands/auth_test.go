package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/internal/auth"
)

func TestAuthCommands_Disabled(t *testing.T) {
	t.Setenv("TEST_GO_AUTH", "1")
	cnf := testConfig()
	cnf.Application.DisabledCommands = []string{"auth:api-token-login"}
	cnf.Application.WrappedDisabledCommands = []string{"auth:token"}
	names := make([]string, 0, 2)
	for _, c := range authCommands(cnf) {
		names = append(names, c.Name())
	}
	assert.Equal(t, []string{"auth:browser-login", "auth:logout"}, names)
}

func TestAuthCommands_LegacyGlobalFlags(t *testing.T) {
	t.Setenv("TEST_GO_AUTH", "1")
	root := newRootCommand(testConfig())
	for _, args := range [][]string{
		{"logout", "-n"},
		{"auth:token", "-W", "--no-ansi"},
		{"login", "--ansi", "--no"},
	} {
		c, rest, err := root.Find(args)
		require.NoError(t, err)
		assert.NoError(t, c.ParseFlags(rest), "args: %v", args)
	}
}

func TestLoginRequiredMessage(t *testing.T) {
	maxAge := 300
	cases := []struct {
		name string
		err  auth.LoginRequiredError
		want string
	}{
		{
			name: "no challenge",
			want: "Authentication is required.\n\nPlease log in by running:\n    %s login",
		},
		{
			name: "methods and max age",
			err:  auth.LoginRequiredError{AuthMethods: []string{"sso:acme corp", "mfa"}, MaxAge: &maxAge},
			want: "More recent authentication is required.\n\nPlease log in by running:\n" +
				"    %s login --method 'sso:acme corp,mfa' --max-age 300",
		},
		{
			name: "MFA with an API token",
			err:  auth.LoginRequiredError{AuthMethods: []string{"mfa"}, HasAPIToken: true},
			want: "Multi-factor authentication (MFA) is required.\n\n" +
				"The API token may need to be re-created after enabling MFA.",
		},
		{
			name: "SSO with an API token",
			err:  auth.LoginRequiredError{AuthMethods: []string{"sso:acme"}, HasAPIToken: true},
			want: "Single sign-on (SSO) is required.",
		},
	}
	cnf := testConfig()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			want := c.want
			if strings.Contains(want, "%s") {
				want = fmt.Sprintf(want, cnf.Application.Executable)
			}
			assert.Equal(t, want, loginRequiredMessage(cnf, &c.err))
		})
	}
}

func TestAuthCommands_GoAuthDisabled(t *testing.T) {
	t.Setenv("TEST_GO_AUTH", "")
	assert.Empty(t, authCommands(testConfig()))
	root := newRootCommand(testConfig())
	c, _, err := root.Find([]string{"auth:internal"})
	require.NoError(t, err)
	assert.Equal(t, root, c, "auth:internal must not be registered")
}

func TestBrowserCommand_Whitespace(t *testing.T) {
	assert.Nil(t, browserCommand("0"))
	assert.NotPanics(t, func() { browserCommand("  ") })
}

// setWSLInterop fakes WSL's binfmt_misc entry for Windows interop (an empty state leaves it out), wsl.conf (missing),
// and the default automount root. It returns the paths for wsl.conf and System32's rundll32.exe (missing).
func setWSLInterop(t *testing.T, state string) (conf, rundll string) {
	dir := t.TempDir()
	if state != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "WSLInterop"), []byte(state+"\ninterpreter /init\n"), 0o600))
	}
	origDir, origConf, origRoot := wslBinfmtDir, wslConfPath, wslDefaultMountRoot
	wslBinfmtDir, wslConfPath, wslDefaultMountRoot = dir, filepath.Join(dir, "wsl.conf"), dir+"/"
	t.Cleanup(func() { wslBinfmtDir, wslConfPath, wslDefaultMountRoot = origDir, origConf, origRoot })
	return wslConfPath, filepath.Join(dir, "c", "Windows", "System32", "rundll32.exe")
}

// wslOpener returns the program wslBrowserCommand finds, or an empty string.
func wslOpener() string {
	if c := wslBrowserCommand(); c != nil {
		return c[0]
	}
	return ""
}

// writeStub writes an executable stub, creating its directory.
func writeStub(t *testing.T, path string) {
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700)) //nolint:gosec // an executable stub
}

// TestWSLInteropEnabled covers WSL2, which keeps the binfmt_misc entry enabled when wsl.conf disables interop.
func TestWSLInteropEnabled(t *testing.T) {
	cases := []struct {
		name    string
		binfmt  string
		wslConf string
		want    bool
	}{
		{name: "enabled", binfmt: "enabled", want: true},
		{name: "no entry", want: false},
		{name: "entry disabled", binfmt: "disabled", want: false},
		{name: "wsl.conf disables", binfmt: "enabled", wslConf: "[boot]\nsystemd=true\n[interop]\nenabled=false\n"},
		{name: "wsl.conf spacing and case", binfmt: "enabled", wslConf: "[Interop]\n  Enabled = False # off\n"},
		{name: "wsl.conf quoted", binfmt: "enabled", wslConf: "[interop]\nenabled = \"false\"\n"},
		{name: "wsl.conf enables", binfmt: "enabled", wslConf: "[interop]\nenabled=true\n", want: true},
		{name: "other section", binfmt: "enabled", wslConf: "[automount]\nenabled=false\n", want: true},
		{name: "comment", binfmt: "enabled", wslConf: "[interop]\n# enabled=false\n", want: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setWSLInterop(t, c.binfmt)
			assert.Equal(t, c.want, wslInteropEnabled(c.wslConf))
		})
	}
}

func TestBrowserCommand_WSL(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("WSL is Linux")
	}
	_, system32 := setWSLInterop(t, "enabled")
	writeStub(t, system32)
	t.Setenv("DISPLAY", "")
	t.Setenv("WSL_INTEROP", "")
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")

	assert.True(t, isWSL())
	assert.Equal(t, system32, wslOpener())
	assert.True(t, hasDisplay(), "a Windows browser can be used without a display")
	assert.Equal(t, []string{system32, "url.dll,FileProtocolHandler"}, browserCommand(""))
	assert.True(t, canOpenURLs(""))
}

// TestWSLBrowser_MountRoot checks that rundll32.exe is found via wsl.conf's automount root, or else via PATH.
func TestWSLBrowser_MountRoot(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("WSL is Linux")
	}
	custom := t.TempDir()
	customRundll := filepath.Join(custom, "c", "Windows", "System32", "rundll32.exe")
	cases := []struct {
		name    string
		wslConf string
		stub    string // Where to write rundll32.exe: "default", "custom" (roots), "path", or nowhere.
		want    string // Where it is expected to be found, as above, or nowhere.
	}{
		{name: "default root", stub: "default", want: "default"},
		{name: "custom root", wslConf: "[automount]\nroot = " + custom + "/\n", stub: "custom", want: "custom"},
		{name: "custom root without slash", wslConf: "[Automount]\nRoot=" + custom + "\n", stub: "custom", want: "custom"},
		{name: "custom root, quoted", wslConf: "[automount]\nroot = \"" + custom + "/\"\n", stub: "custom", want: "custom"},
		{name: "custom root, rundll32 in default root", wslConf: "[automount]\nroot=" + custom + "/\n", stub: "default"},
		{name: "rundll32 in PATH", stub: "path", want: "path"},
		{name: "no rundll32"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			conf, defaultRundll := setWSLInterop(t, "enabled")
			if c.wslConf != "" {
				require.NoError(t, os.WriteFile(conf, []byte(c.wslConf), 0o600))
			}
			require.NoError(t, os.RemoveAll(filepath.Join(custom, "c")))
			pathDir := t.TempDir()
			t.Setenv("PATH", pathDir)
			t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
			switch c.stub {
			case "default":
				writeStub(t, defaultRundll)
			case "custom":
				writeStub(t, customRundll)
			case "path":
				writeStub(t, filepath.Join(pathDir, "rundll32.exe"))
			}

			switch c.want {
			case "default":
				assert.Equal(t, defaultRundll, wslOpener())
			case "custom":
				assert.Equal(t, customRundll, wslOpener())
			case "path":
				assert.Equal(t, filepath.Join(pathDir, "rundll32.exe"), wslOpener())
			default:
				assert.Empty(t, wslOpener())
				assert.Nil(t, wslBrowserCommand())
			}
		})
	}
}

func TestWSLConfValue(t *testing.T) {
	conf := "[boot]\nsystemd=true\nroot=/boot/\n\n[Automount] # mounts\n  Root = /win/ # comment\n"
	assert.Equal(t, "true", wslConfValue(conf, "boot", "systemd"))
	assert.Equal(t, "/win/", wslConfValue(conf, "automount", "root"))
	assert.Empty(t, wslConfValue(conf, "interop", "enabled"))
	assert.Empty(t, wslConfValue("", "automount", "root"))

	quoted := "[a]\nd=\"x\"\ns='y'\nm=\"z'\nq=\"\n"
	assert.Equal(t, "x", wslConfValue(quoted, "a", "d"))
	assert.Equal(t, "y", wslConfValue(quoted, "a", "s"))
	assert.Equal(t, "\"z'", wslConfValue(quoted, "a", "m"))
	assert.Equal(t, "\"", wslConfValue(quoted, "a", "q"))
}

// TestBrowserCommand_WSLWithoutOpener checks WSL without a Windows opener, e.g. with interop disabled, or a Docker
// Desktop container (which shares the WSL kernel).
func TestBrowserCommand_WSLWithoutOpener(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("WSL is Linux")
	}
	setWSLInterop(t, "enabled")
	t.Setenv("PATH", t.TempDir())
	t.Setenv("DISPLAY", "")
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")

	assert.False(t, hasDisplay())
	assert.False(t, canOpenURLs(""))
}

// TestBrowserCommand_WSLInteropOff checks that a Windows opener is not used when interop is off, as it cannot run.
func TestBrowserCommand_WSLInteropOff(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("WSL is Linux")
	}
	t.Setenv("DISPLAY", "")
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")

	for _, state := range []string{"", "disabled"} {
		_, rundll := setWSLInterop(t, state)
		writeStub(t, rundll)
		assert.False(t, hasDisplay(), "interop state %q", state)
		assert.Empty(t, wslOpener(), "interop state %q", state)
		assert.False(t, canOpenURLs(""), "interop state %q", state)
	}
}

func TestIsWSL_NotWSL(t *testing.T) {
	if b, _ := os.ReadFile("/proc/sys/kernel/osrelease"); strings.Contains(strings.ToLower(string(b)), "microsoft") {
		t.Skip("running in WSL")
	}
	t.Setenv("WSL_DISTRO_NAME", "")
	t.Setenv("WSL_INTEROP", "")
	assert.False(t, isWSL())
}
