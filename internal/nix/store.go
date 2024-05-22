package nix

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"go.jetpack.io/devbox/internal/debug"
	"go.jetpack.io/devbox/internal/redact"
	"golang.org/x/exp/maps"
)

func StorePathFromHashPart(ctx context.Context, hash, storeAddr string) (string, error) {
	cmd := commandContext(ctx, "store", "path-from-hash-part", "--store", storeAddr, hash)
	resultBytes, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(resultBytes)), nil
}

func StorePathsFromInstallable(ctx context.Context, installable string, allowInsecure bool) ([]string, error) {
	defer debug.FunctionTimer().End()
	// --impure for NIXPKGS_ALLOW_UNFREE
	cmd := commandContext(ctx, "path-info", installable, "--json", "--impure")
	cmd.Env = allowUnfreeEnv(os.Environ())

	if allowInsecure {
		debug.Log("Setting Allow-insecure env-var\n")
		cmd.Env = allowInsecureEnv(cmd.Env)
	}

	debug.Log("Running cmd %s", cmd)
	resultBytes, err := cmd.Output()
	if err != nil {
		if exitErr := (&exec.ExitError{}); errors.As(err, &exitErr) {
			return nil, redact.Errorf(
				"nix path-info exit code: %d, output: %s, err: %w",
				redact.Safe(exitErr.ExitCode()),
				exitErr.Stderr,
				err,
			)
		}

		return nil, err
	}
	return parseStorePathFromInstallableOutput(installable, resultBytes)
}

// StorePathsAreInStore returns true if the store path is in the store
// It relies on `nix store ls` to check if the store path is in the store
func StorePathsAreInStore(ctx context.Context, storePaths []string) (bool, error) {
	defer debug.FunctionTimer().End()
	for _, storePath := range storePaths {
		cmd := commandContext(ctx, "store", "ls", storePath)
		debug.Log("Running cmd %s", cmd)
		if err := cmd.Run(); err != nil {
			if exitErr := (&exec.ExitError{}); errors.As(err, &exitErr) {
				return false, nil
			}
			return false, err
		}
	}
	return true, nil
}

// parseStorePathFromInstallableOutput parses the output of `nix store path-from-installable --json`
// This function is decomposed out of StorePathFromInstallable to make it testable.
func parseStorePathFromInstallableOutput(installable string, output []byte) ([]string, error) {
	// Newer nix versions (like 2.20)
	var out1 map[string]any
	if err := json.Unmarshal(output, &out1); err == nil {
		return maps.Keys(out1), nil
	}

	// Older nix versions (like 2.17)
	var out2 []struct {
		Path  string `json:"path"`
		Valid bool   `json:"valid"`
	}
	if err := json.Unmarshal(output, &out2); err == nil {
		res := []string{}
		for _, outValue := range out2 {
			res = append(res, outValue.Path)
		}
		return res, nil
	}

	return nil, fmt.Errorf("failed to parse store path from installable (%s) output: %s", installable, output)
}

// DaemonError reports an unsuccessful attempt to connect to the Nix daemon.
type DaemonError struct {
	cmd    string
	stderr []byte
	err    error
}

func (e *DaemonError) Error() string {
	if len(e.stderr) != 0 {
		return e.Redact() + ": " + string(e.stderr)
	}
	return e.Redact()
}

func (e *DaemonError) Unwrap() error {
	return e.err
}

func (e *DaemonError) Redact() string {
	// Don't include e.stderr in redacted messages because it can contain
	// things like paths and usernames.
	if e.cmd != "" {
		return fmt.Sprintf("command %s: %s", e.cmd, e.err)
	}
	return e.err.Error()
}

// DaemonVersion returns the version of the currently running Nix daemon.
func DaemonVersion(ctx context.Context) (string, error) {
	// We only need the version to decide which CLI flags to use. We can
	// ignore the error because an empty version assumes nix.MinVersion.
	cliVersion, _ := Version()

	storeCmd := "ping"
	if cliVersion.AtLeast(Version2_19) {
		// "nix store ping" is deprecated as of 2.19 in favor of
		// "nix store info".
		storeCmd = "info"
	}
	canJSON := cliVersion.AtLeast(Version2_14)

	cmd := commandContext(ctx, "store", storeCmd, "--store", "daemon")
	if canJSON {
		cmd.Args = append(cmd.Args, "--json")
	}
	out, err := cmd.Output()

	// ExitError means the command ran, but couldn't connect.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return "", &DaemonError{
			cmd:    cmd.String(),
			stderr: exitErr.Stderr,
			err:    err,
		}
	}

	// All other errors mean we couldn't launch the Nix CLI (either it is
	// missing or not executable).
	if err != nil {
		return "", redact.Errorf("command %s: %s", redact.Safe(cmd), err)
	}

	if len(out) == 0 {
		return "", redact.Errorf("command %s: empty output", redact.Safe(cmd), err)
	}
	if canJSON {
		info := struct{ Version string }{}
		if err := json.Unmarshal(out, &info); err != nil {
			return "", redact.Errorf("command %s: unmarshal JSON output: %s", redact.Safe(cmd), err)
		}
		return info.Version, nil
	}

	// Example output:
	//
	// Store URL: daemon
	// Version: 2.21.1
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		name, value, found := strings.Cut(line, ": ")
		if found && name == "Version" {
			return value, nil
		}
	}
	return "", redact.Errorf("parse nix daemon version: %s", redact.Safe(lines[0]))
}
