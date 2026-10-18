package platformify

import (
	"fmt"
)

// detected prints a detected property of the project.
func (w *wizard) detected(what, value string) {
	fmt.Fprintf(w.stderr, "%s %s\n", colorize(greenCode, "✓"), colorize(brandCode, "Detected "+what+": "+value))
}

func (w *wizard) welcome() error {
	fmt.Fprintln(w.stdout, colorize(brandCode, fmt.Sprintf("Welcome to %s!", w.assets.ServiceName)))
	fmt.Fprintln(w.stdout, colorize(brandCode, "Let's get started with a few questions."))
	fmt.Fprintln(w.stdout)
	fmt.Fprintln(w.stdout, "We need to know a bit more about your project. This will only take a minute!")
	fmt.Fprintln(w.stdout)
	return nil
}

func (w *wizard) almostDone() error {
	fmt.Fprintln(w.stdout)
	fmt.Fprintln(w.stdout, colorize(accentCode, "                       (\\_/)"))
	fmt.Fprintln(w.stdout, colorize(accentCode, "We’re almost done...  =(^.^)="))
	fmt.Fprintln(w.stdout)
	return nil
}

func (w *wizard) done(files []string) {
	out := w.stdout
	fmt.Fprintln(out)
	fmt.Fprintln(out, "┌───────────────────────────────────────────────────┐")
	fmt.Fprintln(out, "│   CONGRATULATIONS!                                │")
	fmt.Fprintln(out, "│                                                   │")
	fmt.Fprintln(out, "│   We have created the following files for your:   │")
	for _, f := range files {
		fmt.Fprintf(out, "│     - %-44s│\n", f)
	}
	fmt.Fprintln(out, "│                                                   │")
	fmt.Fprintln(out, "│   We’re jumping for joy! ⍢                        │")
	fmt.Fprintln(out, "└───────────────────────────────────────────────────┘")
	fmt.Fprintln(out, "         │ /")
	fmt.Fprintln(out, "         │/")
	fmt.Fprintln(out, "         │")
	fmt.Fprintln(out, "  (\\ /)")
	fmt.Fprintln(out, "  ( . .)")
	fmt.Fprintln(out, "  o (_(“)(“)")
	fmt.Fprintln(out)
	fmt.Fprintln(out, colorize(accentCode, "You can now deploy your application to "+w.assets.ServiceName+"!"))
	fmt.Fprintln(out, colorize(accentCode, fmt.Sprintf(
		"To do so, commit your files and deploy your application using the %s CLI:", w.assets.ServiceName)))
	fmt.Fprintln(out, "  $ git add .")
	fmt.Fprintf(out, "  $ git commit -m 'Add %s configuration files'\n", w.assets.ServiceName)
	fmt.Fprintf(out, "  $ %s project:set-remote\n", w.assets.Binary)
	fmt.Fprintf(out, "  $ %s push\n", w.assets.Binary)
	fmt.Fprintln(out)
}

// suggestLaravelBridge suggests installing the Laravel Bridge, if it is not required.
func (w *wizard) suggestLaravelBridge() {
	composerFiles := findAllFiles(w.appDir(), composerJSON)
	for _, p := range composerFiles {
		if _, ok := jsonValue(p, "require", "platformsh/laravel-bridge"); ok {
			return
		}
	}
	if len(composerFiles) == 0 {
		return
	}
	fmt.Fprintln(w.stdout, colorize(warningCode,
		"\nPlease use composer to add the Laravel Bridge to your project:\n\n"+
			"    composer require platformsh/laravel-bridge\n"))
}

// assets are the vendor-specific values used in messages and templates.
type assets struct {
	Binary      string
	EnvPrefix   string
	ServiceName string
	docsBaseURL string
	flex        bool
}

// docs are documentation links used in templates.
type docs struct {
	AppReference string
	Hooks        string
	PHP          string
	Routes       string
	Services     string
	SymfonyCLI   string
	TimeZone     string
	Variables    string
}

// Docs returns documentation links.
func (a *assets) Docs() docs {
	return docs{
		AppReference: a.docsBaseURL + "/app/reference/",
		Hooks:        a.docsBaseURL + "/app/hooks/compare/",
		PHP:          a.docsBaseURL + "/languages/php/",
		Routes:       a.docsBaseURL + "/routes/",
		Services:     a.docsBaseURL + "/services/",
		SymfonyCLI:   a.docsBaseURL + "/stacks/symfony/cli/tips/",
		TimeZone:     a.docsBaseURL + "/app/timezone/",
		Variables:    a.docsBaseURL + "/variables/use/provided/",
	}
}
