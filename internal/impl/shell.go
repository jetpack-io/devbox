// Copyright 2023 Jetpack Technologies Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package impl

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/alessio/shellescape"
	"github.com/pkg/errors"
	"go.jetpack.io/devbox/internal/telemetry"

	"go.jetpack.io/devbox/internal/debug"
	"go.jetpack.io/devbox/internal/envir"
	"go.jetpack.io/devbox/internal/nix"
	"go.jetpack.io/devbox/internal/xdg"
)

//go:embed shellrc.tmpl
var shellrcText string
var shellrcTmpl = template.Must(template.New("shellrc").Parse(shellrcText))

//go:embed shellrc_fish.tmpl
var fishrcText string
var fishrcTmpl = template.Must(template.New("shellrc_fish").Parse(fishrcText))

type name string

const (
	shUnknown name = ""
	shBash    name = "bash"
	shZsh     name = "zsh"
	shKsh     name = "ksh"
	shFish    name = "fish"
	shPosix   name = "posix"
)

var ErrNoRecognizableShellFound = errors.New("SHELL in undefined, and couldn't find any common shells in PATH")

// TODO consider splitting this struct's functionality so that there is a simpler
// `nix.Shell` that can produce a raw nix shell once again.

// DevboxShell configures a user's shell to run in Devbox. Its zero value is a
// fallback shell that launches a regular Nix shell.
type DevboxShell struct {
	name            name
	binPath         string
	projectDir      string // path to where devbox.json config resides
	env             map[string]string
	userShellrcPath string

	hooksFilePath string

	// profileDir is the absolute path to the directory storing the nix-profile
	profileDir  string
	historyFile string

	// shellStartTime is the unix timestamp for when the command was invoked
	shellStartTime time.Time
}

type ShellOption func(*DevboxShell)

// NewDevboxShell initializes the DevboxShell struct so it can be used to start a shell environment
// for the devbox project.
func NewDevboxShell(devbox *Devbox, opts ...ShellOption) (*DevboxShell, error) {
	shPath, err := shellPath(devbox)
	if err != nil {
		return nil, err
	}
	sh := initShellBinaryFields(shPath)

	for _, opt := range opts {
		opt(sh)
	}

	debug.Log("Recognized shell as: %s", sh.binPath)
	debug.Log("Looking for user's shell init file at: %s", sh.userShellrcPath)
	return sh, nil
}

// shellPath returns the path to a shell binary, or error if none found.
func shellPath(devbox *Devbox) (path string, err error) {
	defer func() {
		if err != nil {
			path = filepath.Clean(path)
		}
	}()

	if !devbox.pure {
		// First, check the SHELL environment variable.
		path = os.Getenv(envir.Shell)
		if path != "" {
			debug.Log("Using SHELL env var for shell binary path: %s\n", path)
			return path, nil
		}
	}

	// Second, fallback to using the bash that nix uses by default.

	var bashNixStorePath string // of the form /nix/store/{hash}-bash-{version}/

	cmd := nix.Command(context.TODO(), "eval", "--raw",
		fmt.Sprintf("%s#bashInteractive", nix.FlakeNixpkgs(devbox.cfg.NixPkgsCommitHash())),
	)
	out, err := cmd.Output()
	if err != nil {
		return "", errors.WithStack(err)
	}
	bashNixStorePath = string(out)

	// install bashInteractive in nix/store without creating a symlink to local directory (--no-link)
	cmd = nix.Command(context.TODO(), "build", bashNixStorePath, "--no-link")
	err = cmd.Run()
	if err != nil {
		return "", errors.WithStack(err)
	}

	if bashNixStorePath != "" {
		// the output is the raw path to the bash installation in the /nix/store
		return fmt.Sprintf("%s/bin/bash", bashNixStorePath), nil
	}

	// Else, return an error
	return "", ErrNoRecognizableShellFound
}

// initShellBinaryFields initializes the fields specific to the shell binary that will be used
// for the devbox shell.
func initShellBinaryFields(path string) *DevboxShell {
	shell := &DevboxShell{binPath: path}
	base := filepath.Base(path)
	// Login shell
	if base[0] == '-' {
		base = base[1:]
	}
	switch base {
	case "bash":
		shell.name = shBash
		shell.userShellrcPath = rcfilePath(".bashrc")
	case "zsh":
		shell.name = shZsh
		shell.userShellrcPath = rcfilePath(".zshrc")
	case "ksh":
		shell.name = shKsh
		shell.userShellrcPath = rcfilePath(".kshrc")
	case "fish":
		shell.name = shFish
		shell.userShellrcPath = fishConfig()
	case "dash", "ash", "shell":
		shell.name = shPosix
		shell.userShellrcPath = os.Getenv(envir.Env)

		// Just make up a name if there isn't already an init file set
		// so we have somewhere to put a new one.
		if shell.userShellrcPath == "" {
			shell.userShellrcPath = ".shinit"
		}
	default:
		shell.name = shUnknown
	}
	return shell
}

func WithProfile(profileDir string) ShellOption {
	return func(s *DevboxShell) {
		s.profileDir = profileDir
	}
}

func WithHistoryFile(historyFile string) ShellOption {
	return func(s *DevboxShell) {
		s.historyFile = historyFile
	}
}

func WithHooksFilePath(hooksFilePath string) ShellOption {
	return func(s *DevboxShell) {
		s.hooksFilePath = hooksFilePath
	}
}

// TODO: Consider removing this once plugins add env vars directly to binaries via wrapper scripts.
func WithEnvVariables(envVariables map[string]string) ShellOption {
	return func(s *DevboxShell) {
		s.env = envVariables
	}
}

func WithProjectDir(projectDir string) ShellOption {
	return func(s *DevboxShell) {
		s.projectDir = projectDir
	}
}

func WithShellStartTime(t time.Time) ShellOption {
	return func(s *DevboxShell) {
		s.shellStartTime = t
	}
}

// rcfilePath returns the absolute path for an rcfile, which is usually in the
// user's home directory. It doesn't guarantee that the file exists.
func rcfilePath(basename string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, basename)
}

func fishConfig() string {
	return xdg.ConfigSubpath("fish/config.fish")
}

func (s *DevboxShell) Run() error {
	var cmd *exec.Cmd
	shellrc, err := s.writeDevboxShellrc()
	if err != nil {
		// We don't have a good fallback here, since all the variables we need for anything to work
		// are in the shellrc file. For now let's fail. Later on, we should remove the vars from the
		// shellrc file. That said, one of the variables we have to evaluate ($shellHook), so we need
		// the shellrc file anyway (unless we remove the hook somehow).
		debug.Log("Failed to write devbox shellrc: %s", err)
		return errors.WithStack(err)
	}

	// Link other files that affect the shell settings and environments.
	s.linkShellStartupFiles(filepath.Dir(shellrc))
	extraEnv, extraArgs := s.shellRCOverrides(shellrc)
	env := s.env
	for k, v := range extraEnv {
		env[k] = v
	}

	cmd = exec.Command(s.binPath)
	cmd.Env = mapToPairs(env)
	cmd.Args = append(cmd.Args, extraArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	debug.Log("Executing shell %s with args: %v", s.binPath, cmd.Args)
	err = cmd.Run()

	// If the error is an ExitError, this means the shell started up fine but there was
	// an error from executing a shell command or script.
	//
	// This could be from one of the generated shellrc commands, but more likely is from
	// a user's command or script. So, we want to return nil for this.
	if exitErr := (&exec.ExitError{}); errors.As(err, &exitErr) {
		return nil
	}

	// This means that there was an error from devbox's code or nix's code. Not a user
	// error and so we do return it.
	return errors.WithStack(err)
}

func (s *DevboxShell) shellRCOverrides(shellrc string) (extraEnv map[string]string, extraArgs []string) {
	// Shells have different ways of overriding the shellrc, so we need to
	// look at the name to know which env vars or args to set when launching the shell.
	switch s.name {
	case shBash:
		extraArgs = []string{"--rcfile", shellescape.Quote(shellrc)}
	case shZsh:
		extraEnv = map[string]string{"ZDOTDIR": shellescape.Quote(filepath.Dir(shellrc))}
	case shKsh, shPosix:
		extraEnv = map[string]string{"ENV": shellescape.Quote(shellrc)}
	case shFish:
		extraArgs = []string{"-C", ". " + shellrc}
	}
	return extraEnv, extraArgs
}

func (s *DevboxShell) writeDevboxShellrc() (path string, err error) {
	// We need a temp dir (as opposed to a temp file) because zsh uses
	// ZDOTDIR to point to a new directory containing the .zshrc.
	tmp, err := os.MkdirTemp("", "devbox")
	if err != nil {
		return "", fmt.Errorf("create temp dir for shell init file: %v", err)
	}

	// This is a best-effort to include the user's existing shellrc.
	userShellrc := []byte{}
	if s.userShellrcPath != "" {
		// If we can't read it, then just omit it from the devbox shellrc.
		userShellrc, _ = os.ReadFile(s.userShellrcPath)
	}

	// If the user already has a shellrc file, then give the devbox shellrc
	// file the same name. Otherwise, use an arbitrary name of "shellrc".
	shellrcName := "shellrc"
	if s.userShellrcPath != "" {
		shellrcName = filepath.Base(s.userShellrcPath)
	}
	path = filepath.Join(tmp, shellrcName)
	shellrcf, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("write to shell init file: %v", err)
	}
	defer func() {
		cerr := shellrcf.Close()
		if err == nil {
			err = cerr
		}
	}()

	tmpl := shellrcTmpl
	if s.name == shFish {
		tmpl = fishrcTmpl
	}

	err = tmpl.Execute(shellrcf, struct {
		ProjectDir       string
		OriginalInit     string
		OriginalInitPath string
		HooksFilePath    string
		ShellStartTime   string
		HistoryFile      string
		ExportEnv        string
	}{
		ProjectDir:       s.projectDir,
		OriginalInit:     string(bytes.TrimSpace(userShellrc)),
		OriginalInitPath: s.userShellrcPath,
		HooksFilePath:    s.hooksFilePath,
		ShellStartTime:   telemetry.FormatShellStart(s.shellStartTime),
		HistoryFile:      strings.TrimSpace(s.historyFile),
		ExportEnv:        exportify(s.env),
	})
	if err != nil {
		return "", fmt.Errorf("execute shellrc template: %v", err)
	}

	debug.Log("Wrote devbox shellrc to: %s", path)
	return path, nil
}

// linkShellStartupFiles will link files used by the shell for initialization.
// We choose to link instead of copy so that changes made outside can be reflected
// within the devbox shell.
//
// We do not link the .{shell}rc files, since devbox modifies them. See writeDevboxShellrc
func (s *DevboxShell) linkShellStartupFiles(shellSettingsDir string) {
	// For now, we only need to do this for zsh shell
	if s.name == shZsh {
		// Useful explanation of zsh startup files: https://zsh.sourceforge.io/FAQ/zshfaq03.html#l20
		filenames := []string{".zshenv", ".zprofile", ".zlogin"}
		for _, filename := range filenames {
			fileOld := filepath.Join(filepath.Dir(s.userShellrcPath), filename)
			_, err := os.Stat(fileOld)
			if errors.Is(err, fs.ErrNotExist) {
				// this file may not be relevant for the user's setup.
				continue
			}
			if err != nil {
				debug.Log("os.Stat error for %s is %v", fileOld, err)
			}

			fileNew := filepath.Join(shellSettingsDir, filename)

			if err := os.Link(fileOld, fileNew); err == nil {
				debug.Log("Linked shell startup file %s to %s", fileOld, fileNew)
			} else {
				// This is a best-effort operation. If there's an error then log it for visibility but continue.
				debug.Log("Error linking zsh setting file from %s to %s: %v", fileOld, fileNew, err)
			}
		}
	}
}

// JoinPathLists joins and cleans PATH-style strings of
// [os.ListSeparator] delimited paths. To clean a path list, it splits it and
// does the following for each element:
//
//  1. Applies [filepath.Clean].
//  2. Removes the path if it's relative (must begin with '/' and not be '.').
//  3. Removes the path if it's a duplicate.
func JoinPathLists(pathLists ...string) string {
	if len(pathLists) == 0 {
		return ""
	}

	seen := make(map[string]bool)
	var cleaned []string
	for _, path := range pathLists {
		for _, path := range filepath.SplitList(path) {
			path = filepath.Clean(path)
			if path == "." || path[0] != '/' {
				// Remove empty paths and don't allow relative
				// paths for security reasons.
				continue
			}
			if !seen[path] {
				cleaned = append(cleaned, path)
			}
			seen[path] = true
		}
	}
	return strings.Join(cleaned, string(filepath.ListSeparator))
}

func filterPathList(pathList string, keep func(string) bool) string {
	filtered := []string{}
	for _, path := range filepath.SplitList(pathList) {
		if keep(path) {
			filtered = append(filtered, path)
		}
	}
	return strings.Join(filtered, string(filepath.ListSeparator))
}
