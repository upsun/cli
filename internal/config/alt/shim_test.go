package alt

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateExecutable(t *testing.T) {
	cases := []struct {
		name       string
		goos       string
		targetPath string
		configPath string
		want       string
	}{
		{
			name:       "unix",
			goos:       "linux",
			targetPath: "/opt/homebrew/bin/upsun",
			configPath: "/home/u/.config/upsun/vendor.yaml",
			want: "#!/bin/sh\n" +
				"# Generated\n" +
				"export CLI_CONFIG_FILE=/home/u/.config/upsun/vendor.yaml\n" +
				`[ -x /opt/homebrew/bin/upsun ] && exec /opt/homebrew/bin/upsun "$@"` + "\n" +
				`exec upsun "$@"` + "\n",
		},
		{
			name:       "unix with special characters",
			goos:       "darwin",
			targetPath: "/Users/o'neil/my bin/upsun",
			configPath: "/Users/o'neil/$HOME/vendor.yaml",
			want: "#!/bin/sh\n" +
				"# Generated\n" +
				`export CLI_CONFIG_FILE='/Users/o'\''neil/$HOME/vendor.yaml'` + "\n" +
				`[ -x '/Users/o'\''neil/my bin/upsun' ] && exec '/Users/o'\''neil/my bin/upsun' "$@"` + "\n" +
				`exec upsun "$@"` + "\n",
		},
		{
			name:       "unix without a target path",
			goos:       "linux",
			configPath: "/home/u/vendor.yaml",
			want: "#!/bin/sh\n" +
				"# Generated\n" +
				"export CLI_CONFIG_FILE=/home/u/vendor.yaml\n" +
				`exec upsun "$@"` + "\n",
		},
		{
			name:       "windows",
			goos:       "windows",
			targetPath: `C:\Users\A B\scoop\shims\upsun.exe`,
			configPath: `C:\Users\A B\AppData\Roaming\upsun\100%.yaml`,
			want: ":: Generated\r\n" +
				"@echo off\r\n" +
				"setlocal\r\n" +
				`set "CLI_CONFIG_FILE=C:\Users\A B\AppData\Roaming\upsun\100%%.yaml"` + "\r\n" +
				`if not exist "C:\Users\A B\scoop\shims\upsun.exe" goto fallback` + "\r\n" +
				`"C:\Users\A B\scoop\shims\upsun.exe" %*` + "\r\n" +
				"exit /b %ERRORLEVEL%\r\n" +
				":fallback\r\n" +
				"upsun %*\r\n",
		},
		{
			name:       "windows without a target path",
			goos:       "windows",
			configPath: `C:\upsun\vendor.yaml`,
			want: ":: Generated\r\n" +
				"@echo off\r\n" +
				"setlocal\r\n" +
				`set "CLI_CONFIG_FILE=C:\upsun\vendor.yaml"` + "\r\n" +
				"upsun %*\r\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := New("", "Generated", "upsun", c.targetPath, c.configPath, nil)
			assert.Equal(t, c.want, a.generateExecutable(c.goos))
		})
	}
}
