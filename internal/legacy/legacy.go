package legacy

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/gofrs/flock"
	"golang.org/x/sync/errgroup"

	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/internal/file"
)

//go:embed archives/platform.phar
var phar []byte

var PHPVersion = "0.0.0"

// PHPPath, if set at build time, replaces the embedded PHP binary, e.g. with legacy/scripts/coverage/php for coverage.
var PHPPath = ""

const configBasename = "config.yaml"

// CLIWrapper wraps the legacy CLI
type CLIWrapper struct {
	Stdout             io.Writer
	Stderr             io.Writer
	Stdin              io.Reader
	Config             *config.Config
	Version            string
	Debug              bool
	DisableInteraction bool
	ForceColor         bool
	DebugLogFunc       func(string, ...any)
	// PrepareCmd changes a command before it runs, e.g. its environment or extra files, and returns functions to call
	// after it starts and after it exits. It may be nil.
	PrepareCmd func(cmd *exec.Cmd) (started, done func(), err error)

	initOnce  sync.Once
	_cacheDir string
}

func (c *CLIWrapper) debug(msg string, args ...any) {
	if c.DebugLogFunc != nil {
		c.DebugLogFunc(msg, args...)
	}
}

func (c *CLIWrapper) cacheDir() (string, error) {
	if c._cacheDir == "" {
		cd, err := c.Config.TempDir()
		if err != nil {
			return "", err
		}
		cd = filepath.Join(cd, "legacy-"+c.Version)
		if err := os.Mkdir(cd, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return "", err
		}
		c._cacheDir = cd
	}

	return c._cacheDir, nil
}

// runInitOnce runs the init method, only once for this object.
func (c *CLIWrapper) runInitOnce() error {
	var err error
	c.initOnce.Do(func() { err = c.init() })
	return err
}

// init initializes the CLI wrapper, creating a temporary directory and copying over files.
func (c *CLIWrapper) init() error {
	preInit := time.Now()

	cacheDir, err := c.cacheDir()
	if err != nil {
		return err
	}

	preLock := time.Now()
	fileLock := flock.New(filepath.Join(cacheDir, ".lock"))
	if err := fileLock.Lock(); err != nil {
		return fmt.Errorf("could not acquire lock: %w", err)
	}
	c.debug("lock acquired (%s): %s", time.Since(preLock), fileLock.Path())
	defer fileLock.Unlock() //nolint:errcheck

	g := errgroup.Group{}
	if c.customPharPath() == "" {
		g.Go(func() error {
			if err := file.WriteIfNeeded(c.pharPath(cacheDir), phar, 0o644); err != nil {
				return fmt.Errorf("could not copy phar file: %w", err)
			}
			return nil
		})
	}
	g.Go(func() error {
		configContent, err := c.Config.Raw()
		if err != nil {
			return fmt.Errorf("could not load config for checking: %w", err)
		}
		if err := file.WriteIfNeeded(filepath.Join(cacheDir, configBasename), configContent, 0o644); err != nil {
			return fmt.Errorf("could not write config: %w", err)
		}
		return nil
	})

	phpMgr := newPHPManager(cacheDir)
	g.Go(phpMgr.copy)

	if err := g.Wait(); err != nil {
		return err
	}

	c.debug("Initialized PHP CLI (%s)", time.Since(preInit))

	return nil
}

// Exec a legacy CLI command with the given arguments
func (c *CLIWrapper) Exec(ctx context.Context, args ...string) error {
	if err := c.runInitOnce(); err != nil {
		return fmt.Errorf("failed to initialize PHP CLI: %w", err)
	}
	cacheDir, err := c.cacheDir()
	if err != nil {
		return err
	}
	cmd := c.makeCmd(ctx, args, cacheDir)
	cmd.Stdin = c.Stdin
	cmd.Stdout = c.Stdout
	cmd.Stderr = c.Stderr
	cmd.Env = append(cmd.Env, os.Environ()...)
	envPrefix := c.Config.Application.EnvPrefix
	cmd.Env = append(
		cmd.Env,
		"CLI_CONFIG_FILE="+filepath.Join(cacheDir, configBasename),
		envPrefix+"MIGRATE_CHECK=0",
		envPrefix+"APPLICATION_PROMPT_SELF_INSTALL=0",
		envPrefix+"WRAPPED=1",
		envPrefix+"APPLICATION_VERSION="+c.Version,
	)
	// The legacy CLI gets tokens and auth state by running this executable's hidden auth:internal command.
	if exe, err := os.Executable(); err == nil {
		cmd.Env = append(cmd.Env, envPrefix+"WRAPPER_EXECUTABLE="+exe)
	}
	if c.DisableInteraction {
		cmd.Env = append(cmd.Env, envPrefix+"NO_INTERACTION=1")
	}
	if c.ForceColor {
		cmd.Env = append(cmd.Env, "CLICOLOR_FORCE=1")
	}
	cmd.Env = append(cmd.Env, fmt.Sprintf(
		"%sUSER_AGENT={APP_NAME_DASH}/%s ({UNAME_S}; {UNAME_R}; PHP %s)",
		envPrefix,
		c.Version,
		PHPVersion,
	))
	started := func() {}
	if c.PrepareCmd != nil {
		s, done, err := c.PrepareCmd(cmd)
		if err != nil {
			return err
		}
		defer done()
		started = s
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not run PHP CLI command: %w", err)
	}
	started()
	stop := relaySignals(cmd.Process)
	err = cmd.Wait()
	stop()
	if err != nil {
		return fmt.Errorf("could not run PHP CLI command: %w", err)
	}

	return nil
}

// relaySignals stops the signals that end the PHP process from also ending this one, so that it can clean up after the
// PHP process exits: an interrupt (e.g. Ctrl+C) reaches PHP from the terminal, and other signals are passed on to it.
func relaySignals(p *os.Process) (stop func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case sig := <-ch:
				if sig != os.Interrupt {
					_ = p.Signal(sig)
				}
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}

// makeCmd makes a legacy CLI command with the given context and arguments.
func (c *CLIWrapper) makeCmd(ctx context.Context, args []string, cacheDir string) *exec.Cmd {
	phpMgr := newPHPManager(cacheDir)
	settings := phpMgr.settings()
	// "-n" tells PHP to ignore any php.ini and scan directory on the host.
	// The embedded PHP binary is statically built with all extensions it
	// needs compiled in, and cannot load shared extensions referenced by a
	// system php.ini (e.g. inside Lando containers).
	var cmdArgs = make([]string, 0, len(args)+2+len(settings)*2)
	cmdArgs = append(cmdArgs, "-n")
	for _, s := range settings {
		cmdArgs = append(cmdArgs, "-d", s)
	}
	cmdArgs = append(cmdArgs, c.pharPath(cacheDir))
	cmdArgs = append(cmdArgs, args...)
	bin := phpMgr.binPath()
	if PHPPath != "" {
		bin = PHPPath
	}
	return exec.CommandContext(ctx, bin, cmdArgs...) //nolint:gosec
}

// PharPath returns the path to the legacy CLI's Phar file.
func (c *CLIWrapper) PharPath() (string, error) {
	cacheDir, err := c.cacheDir()
	if err != nil {
		return "", err
	}

	return c.pharPath(cacheDir), nil
}

func (c *CLIWrapper) pharPath(cacheDir string) string {
	if customPath := c.customPharPath(); customPath != "" {
		return customPath
	}

	return filepath.Join(cacheDir, c.Config.Application.Executable+".phar")
}

// customPharPath returns the path to a phar (or PHP script) to run instead of the embedded phar, if one is set.
func (c *CLIWrapper) customPharPath() string {
	return os.Getenv(c.Config.Application.EnvPrefix + "PHAR_PATH")
}
