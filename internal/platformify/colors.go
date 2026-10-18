package platformify

import (
	"os"

	"github.com/AlecAivazis/survey/v2/core"
	"github.com/mgutz/ansi"
)

// Colors, as names for survey templates and as ANSI codes.
var (
	colorBrand     = "210"
	colorAccent    = "33"
	colorSecondary = "247"
	colorError     = "red"
	colorDefault   = "default"

	brandCode   = ansi.ColorCode(colorBrand)
	accentCode  = ansi.ColorCode(colorAccent)
	greenCode   = ansi.ColorCode("green")
	warningCode = ansi.ColorCode("yellow")
	resetCode   = ansi.ColorCode("reset")
)

func init() {
	if v := os.Getenv("NO_COLOR"); v != "" && v != "0" {
		brandCode, accentCode, greenCode, warningCode, resetCode = "", "", "", "", ""
		core.DisableColor = true
	}
}

// colorize wraps s in the given ANSI color code.
func colorize(code, s string) string {
	return code + s + resetCode
}
