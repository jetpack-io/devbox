// Package setup performs setup tasks and records metadata about when they're
// run.
package setup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/AlecAivazis/survey/v2"
	"github.com/mattn/go-isatty"
	"go.jetpack.io/devbox/internal/build"
	"go.jetpack.io/devbox/internal/debug"
	"go.jetpack.io/devbox/internal/envir"
	"go.jetpack.io/devbox/internal/redact"
	"go.jetpack.io/devbox/internal/xdg"
)

// ErrUserRefused indicates that the user responded no to an interactive
// confirmation prompt.
var ErrUserRefused = errors.New("user refused run")

// ErrAlreadyRefused indicates that no confirmation prompt was shown because the
// user previously refused to run the task. Call [Reset] to re-prompt the user
// for confirmation.
var ErrAlreadyRefused = errors.New("already refused by user")

type ctxKey string

// ctxKeyTask tracks the current task key across processes when relaunching
// with sudo.
var ctxKeyTask ctxKey = "task"

// Task is a setup action that can conditionally run based on the state of a
// previous run.
type Task interface {
	Run(ctx context.Context) error

	// NeedsRun returns true if the task needs to be run. It should assume
	// that lastRun persists across executions of the program and is unique
	// for each user.
	//
	// A task that should only run once can check if lastRun.Time is the zero value.
	// A task that only runs after an update can check if lastRun.Version < build.Version.
	// A retryable task can check lastRun.Error to see if the previous run failed.
	NeedsRun(ctx context.Context, lastRun RunInfo) bool
}

// RunInfo contains metadata that describes the most recent run of a task.
type RunInfo struct {
	// Time is the last time the task ran.
	Time time.Time `json:"time"`

	// Version is the version of Devbox that last ran the task.
	Version string `json:"version"`

	// Error is the error message returned by the last run. It's empty if
	// the last run succeeded.
	Error string `json:"error,omitempty"`
}

// Run runs a setup task and stores its state under a given key. Keys are
// namespaced by user. It only calls the task's Run method when NeedsRun returns
// true.
func Run(ctx context.Context, key string, task Task) error {
	return run(ctx, key, task, "")
}

// SudoDevbox relaunches Devbox as root using sudo, taking care to preserve
// Devbox environment variables that can affect the new process. If the current
// user is already root, then it returns (false, nil) to indicate that no sudo
// process ran. The caller can use this as a hint to know if it's running as the
// sudoed process. Typical usage is:
//
//	func (*ConfigTask) Run(context.Context) error {
//		ran, err := SudoDevbox(ctx, "cache", "configure")
//		if ran || err != nil {
//			// return early if we kicked off a sudo process or there
//			// was an error
//			return err
//		}
//		// do things as root
//	}
//
//	ConfirmRun(ctx, key, &ConfigTask{}, "Allow sudo to run Devbox as root?")
//
// A task that calls SudoDevbox should pass command arguments that cause the new
// Devbox process to rerun the task. The task executes unconditionally within
// the sudo process without re-prompting the user or a second call to its
// NeedsRun method.
func SudoDevbox(ctx context.Context, arg ...string) (ran bool, err error) {
	if os.Getuid() == 0 {
		return false, nil
	}

	taskKey := ""
	if v := ctx.Value(ctxKeyTask); v != nil {
		taskKey = v.(string)
	}

	// Ensure the state file and its directory exist before sudoing,
	// otherwise they will be owned by root. This is easier than recursively
	// chowning new directories/files after root creates them.
	if taskKey != "" {
		saveState(taskKey, state{})
	}

	// Use the absolute path to Devbox instead of relying on PATH for two
	// reasons:
	//
	//  1. sudo isn't guaranteed to preserve the current PATH and the root
	//     user might not have devbox in its PATH.
	//  2. If we're running an alternative version of Devbox
	//     (such as a dev build) we want to use the same binary.
	exe, err := devboxExecutable()
	if err != nil {
		return false, err
	}

	sudoArgs := make([]string, 0, len(arg)+4)
	sudoArgs = append(sudoArgs, "--preserve-env="+strings.Join([]string{
		// Keep writing debug logs from the sudo process.
		"DEVBOX_DEBUG",

		// Use the same Devbox API and auth token.
		"DEVBOX_API_TOKEN",
		"DEVBOX_PROD",

		// In case the Devbox version is overridden.
		"DEVBOX_USE_VERSION",

		// Use the same XDG directories for state, caching, etc.
		"XDG_CACHE_HOME",
		"XDG_CONFIG_DIRS",
		"XDG_CONFIG_HOME",
		"XDG_DATA_DIRS",
		"XDG_DATA_HOME",
		"XDG_RUNTIME_DIR",
		"XDG_STATE_HOME",
	}, ","))
	if taskKey != "" {
		sudoArgs = append(sudoArgs, "DEVBOX_SUDO_TASK="+taskKey)
	}
	sudoArgs = append(sudoArgs, "--", exe)
	sudoArgs = append(sudoArgs, arg...)

	cmd := exec.CommandContext(ctx, "sudo", sudoArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if taskKey == "" {
			return false, redact.Errorf("setup: relaunch with sudo: %w", err)
		}
		return false, taskError(taskKey, redact.Errorf("relaunch with sudo: %w", err))
	}
	return true, nil
}

// Run interactively prompts the user to confirm that it's ok to run a setup
// task. It only prompts the user if the task's NeedsRun method returns true. If
// the user refuses to run the task, then ConfirmPrompt will not ask them again.
// Call [Reset] to reset the task's state and re-prompt the user.
func ConfirmRun(ctx context.Context, key string, task Task, prompt string) error {
	if prompt == "" {
		return taskError(key, redact.Errorf("empty confirmation prompt"))
	}
	return run(ctx, key, task, prompt)
}

var defaultPrompt = func(msg string) (response any, err error) {
	if isatty.IsTerminal(os.Stdin.Fd()) {
		err = survey.AskOne(&survey.Confirm{Message: msg}, &response)
		return response, err
	}
	debug.Log("setup: no tty detected, assuming yes to confirmation prompt: %q", msg)
	return true, nil
}

func run(ctx context.Context, key string, task Task, prompt string) error {
	ctx = context.WithValue(ctx, ctxKeyTask, key)

	// DEVBOX_SUDO_TASK is set when a task relaunched Devbox by calling
	// SudoDevbox. If it matches the current task key, then the pre-sudo
	// process is already running this task and we can skip checking
	// task.NeedsRun and prompting the user.
	isSudo := false
	if envTask := os.Getenv("DEVBOX_SUDO_TASK"); envTask != "" {
		isSudo = envTask == key
	}
	state := loadState(key)
	if !isSudo && !task.NeedsRun(ctx, state.LastRun) {
		return nil
	}

	oldState, newState := state, &state
	defer func() {
		if oldState != *newState {
			saveState(key, *newState)
		}
	}()

	if !isSudo && prompt != "" {
		state.ConfirmPrompt.Message = prompt
		if state.ConfirmPrompt.Asked && !state.ConfirmPrompt.Allowed {
			// We've asked before and the user said no.
			return taskError(key, ErrAlreadyRefused)
		}

		resp, err := defaultPrompt(prompt)
		if err != nil {
			return taskError(key, redact.Errorf("prompt for confirmation: %v", err))
		}
		state.ConfirmPrompt.Asked = true
		state.ConfirmPrompt.Allowed, _ = resp.(bool)
		if !state.ConfirmPrompt.Allowed {
			return taskError(key, ErrUserRefused)
		}
	}

	state.LastRun = RunInfo{
		Time:    time.Now(),
		Version: build.Version,
	}
	if err := task.Run(ctx); err != nil {
		state.LastRun.Error = err.Error()
		return taskError(key, err)
	}
	return nil
}

// Reset removes a task's state so that it acts as if it has never run.
func Reset(key string) {
	err := os.Remove(statePath(key))
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		err = taskError(key, fmt.Errorf("remove state file: %v", err))
		debug.Log(err.Error())
	}
}

type state struct {
	ConfirmPrompt confirmPrompt `json:"confirm_prompt,omitempty"`
	LastRun       RunInfo       `json:"last_run,omitempty"`
}

type confirmPrompt struct {
	Message string `json:"message"`
	Asked   bool   `json:"asked"`
	Allowed bool   `json:"allowed"`
}

func loadState(key string) state {
	path := statePath(key)
	b, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			err = taskError(key, fmt.Errorf("load state file: %v", err))
			debug.Log(err.Error())
		}
		return state{}
	}
	loaded := state{}
	if err := json.Unmarshal(b, &loaded); err != nil {
		err = taskError(key, fmt.Errorf("load state file %s: %v", path, err))
		debug.Log(err.Error())
		return state{}
	}
	return loaded
}

func saveState(key string, s state) {
	path := statePath(key)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		err = taskError(key, fmt.Errorf("save state file: %v", err))
		debug.Log(err.Error())
		return
	}

	err = os.MkdirAll(filepath.Dir(path), 0o755)
	if err == nil {
		err = os.WriteFile(path, data, 0o644)
	}
	if err != nil {
		err = taskError(key, fmt.Errorf("save state file: %v", err))
		debug.Log(err.Error())
		return
	}

	sudoUID, sudoGID := os.Getenv("SUDO_UID"), os.Getenv("SUDO_GID")
	if sudoUID != "" || sudoGID != "" {
		uid, err := strconv.Atoi(sudoUID)
		if err != nil {
			uid = -1
		}
		gid, err := strconv.Atoi(sudoGID)
		if err != nil {
			gid = -1
		}
		err = os.Chown(path, uid, gid)
		if err != nil {
			err = taskError(key, fmt.Errorf("chown state file to non-sudo user: %v", err))
			debug.Log(err.Error())
		}
	}
}

func statePath(key string) string {
	dir := xdg.StateSubpath("devbox")
	name := strings.ReplaceAll(key, "/", "-")
	return filepath.Join(dir, name)
}

func taskError(key string, err error) error {
	if err == nil {
		return nil
	}
	return redact.Errorf("setup: task %s: %w", key, err)
}

// devboxExecutable returns the path to the Devbox launcher script or the
// current binary if the launcher is unavailable.
func devboxExecutable() (string, error) {
	if exe := os.Getenv(envir.LauncherPath); exe != "" {
		if abs, err := filepath.Abs(exe); err == nil {
			return abs, nil
		}
	}

	exe, err := os.Executable()
	if err != nil {
		return "", redact.Errorf("get path to devbox executable: %v", err)
	}
	return exe, nil
}
