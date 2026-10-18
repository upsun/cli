// Package platformify generates a starter configuration for a project, by
// detecting its stack and asking a few questions (the guided mode of "init").
package platformify

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/AlecAivazis/survey/v2"

	"github.com/upsun/cli/internal/config"
)

// Options configures Run.
type Options struct {
	// Dir is the project's root directory, where files are written.
	Dir    string
	Stdout io.Writer
	Stderr io.Writer
}

// errAborted stops the wizard without an error.
var errAborted = errors.New("aborted")

type wizard struct {
	dir     string
	stdout  io.Writer
	stderr  io.Writer
	assets  *assets
	answers *answers
}

// Run asks questions about the project in opts.Dir, and writes its configuration files.
func Run(cnf *config.Config, opts Options) error {
	w := &wizard{
		dir:     opts.Dir,
		stdout:  opts.Stdout,
		stderr:  opts.Stderr,
		assets:  newAssets(cnf),
		answers: &answers{},
	}
	steps := []func() error{
		w.confirmOverwrite,
		w.welcome,
		w.detectStack,
		w.askRuntime,
		w.detectDependencyManagers,
		w.setLocations,
		w.setMounts,
		w.askName,
		w.detectApplicationRoot,
		w.setEnvironment,
		w.setBuildSteps,
		w.setDeployCommand,
		w.setSocketFamily,
		w.setWebCommand,
		w.almostDone,
		w.askServices,
	}
	for _, step := range steps {
		if err := step(); err != nil {
			if errors.Is(err, errAborted) {
				return nil
			}
			return err
		}
	}

	files, err := generate(w.dir, w.templateInput())
	if err != nil {
		return fmt.Errorf("could not configure project: %w", err)
	}
	if err := writeFiles(w.dir, files); err != nil {
		return fmt.Errorf("could not configure project: %w", err)
	}
	if w.answers.stack == laravel {
		w.suggestLaravelBridge()
	}
	w.done()
	return nil
}

// writeFiles writes files, keyed by slash-separated paths relative to dir.
func writeFiles(dir string, files map[string][]byte) error {
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, content, 0o644); err != nil { //nolint:gosec // Configuration files are not secret.
			return err
		}
	}
	return nil
}

// confirmOverwrite asks whether to continue, if configuration files exist.
func (w *wizard) confirmOverwrite() error {
	var existing []string
	for _, f := range w.assets.configFiles() {
		if st, err := os.Stat(filepath.Join(w.dir, f)); err == nil && !st.IsDir() {
			existing = append(existing, f)
		}
	}
	if len(existing) == 0 {
		return nil
	}
	fmt.Fprintln(w.stderr, colorize(warningCode, "You are reconfiguring the project at "+w.dir+"."))
	fmt.Fprintln(w.stderr, colorize(warningCode,
		fmt.Sprintf("The following %s files already exist in this directory:", w.assets.ServiceName)))
	for _, f := range existing {
		fmt.Fprintln(w.stderr, colorize(warningCode, "  - "+f))
	}
	proceed := false
	if err := survey.AskOne(&survey.Confirm{Message: "Do you want to overwrite them?"}, &proceed); err != nil {
		return err
	}
	if !proceed {
		return errAborted
	}
	return nil
}

// suggestSymfonyCLI offers to stop, to use the Symfony CLI instead.
func (w *wizard) suggestSymfonyCLI() error {
	useSymfonyCLI := true
	if err := survey.AskOne(&survey.Confirm{
		Message: "It seems like this project uses Symfony full-stack. For a better experience, " +
			"you should use Symfony CLI. Would you like to use it to deploy your project instead?",
		Default: useSymfonyCLI,
	}, &useSymfonyCLI); err != nil {
		return err
	}
	if useSymfonyCLI {
		fmt.Fprintln(w.stderr, colorize(warningCode,
			"Check out the Symfony CLI documentation here: "+w.assets.Docs().SymfonyCLI))
		return errAborted
	}
	return nil
}

// askRuntime sets the runtime from the stack, or asks for it.
func (w *wizard) askRuntime() error {
	a := w.answers
	a.runtime = a.stack.runtime()
	if a.runtime == "" {
		var title string
		options := titles(runtimeChoices)
		if err := survey.AskOne(&survey.Select{
			Message: "What language is your project using? We support the following:",
			Options: options,
		}, &title, survey.WithPageSize(len(options))); err != nil {
			return err
		}
		a.runtime = typeByTitle(runtimeChoices, title)
	} else {
		w.detected("runtime", runtimeTitle(a.runtime))
	}
	if v := versions(a.runtime); len(v) > 0 {
		a.version = v[0]
	}
	return nil
}

func runtimeTitle(typ string) string {
	for _, c := range runtimeChoices {
		if c.typ == typ {
			return c.title
		}
	}
	return typ
}

var (
	invalidNameChars  = regexp.MustCompile("[^a-z0-9_-]")
	consecutiveDashes = regexp.MustCompile("-+")
)

func slugify(s string) string {
	s = invalidNameChars.ReplaceAllLiteralString(strings.ToLower(strings.TrimSpace(s)), "-")
	return consecutiveDashes.ReplaceAllLiteralString(s, "-")
}

// askName asks for the application name.
func (w *wizard) askName() error {
	validSlug := func(val any) error {
		if s, _ := val.(string); s != slugify(s) {
			return fmt.Errorf("%s: the name can only contain lowercase alphanumeric characters, dashes, or underscores", s)
		}
		return nil
	}
	return survey.AskOne(&survey.Input{
		Message: "Tell us your project's application name:",
		Default: slugify(filepath.Base(w.dir)),
	}, &w.answers.name, survey.WithValidator(survey.Required), survey.WithValidator(validSlug))
}

// askServices asks which services the project uses.
func (w *wizard) askServices() error {
	fmt.Fprintln(w.stdout, colorize(accentCode,
		"Last but not least, unless you’re creating a static website, your project uses services. Let’s define them:"))
	fmt.Fprintln(w.stdout)

	var selected []string
	for {
		if err := survey.AskOne(&survey.MultiSelect{
			Message: "Select all the services you are using:",
			Options: titles(serviceChoices),
		}, &selected, survey.WithKeepFilter(true)); err != nil {
			return err
		}
		if len(selected) > 0 {
			break
		}
		proceed := false
		if err := survey.AskOne(&survey.Confirm{
			Message: "You have not selected any service, would you like to proceed anyway?",
		}, &proceed); err != nil {
			return err
		}
		if proceed {
			break
		}
	}

	for _, title := range selected {
		typ := typeByTitle(serviceChoices, title)
		v := versions(typ)
		if len(v) == 0 {
			continue
		}
		s := service{
			Name:         typ,
			Type:         typ + ":" + v[0],
			TypeVersions: v,
		}
		if !slices.Contains(ephemeralServices, typ) {
			s.Disk = diskSizes[0]
			s.DiskSizes = diskSizes
		}
		w.answers.services = append(w.answers.services, s)
	}
	return nil
}
