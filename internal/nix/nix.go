// Copyright 2023 Jetpack Technologies Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package nix

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime/trace"
	"slices"
	"strings"

	"github.com/pkg/errors"
	"go.jetpack.io/devbox/internal/boxcli/featureflag"
	"go.jetpack.io/devbox/internal/boxcli/usererr"
	"go.jetpack.io/devbox/internal/redact"

	"go.jetpack.io/devbox/internal/debug"
)

// ProfilePath contains the contents of the profile generated via `nix-env --profile ProfilePath <command>`
// or `nix profile install --profile ProfilePath <package...>`
// Instead of using directory, prefer using the devbox.ProfileDir() function that ensures the directory exists.
const ProfilePath = ".devbox/nix/profile/default"

type PrintDevEnvOut struct {
	Variables map[string]Variable // the key is the name.
}

type Variable struct {
	Type  string // valid types are var, exported, and array.
	Value any    // can be a string or an array of strings (iff type is array).
}

type PrintDevEnvArgs struct {
	FlakeDir             string
	PrintDevEnvCachePath string
	UsePrintDevEnvCache  bool
}

// PrintDevEnv calls `nix print-dev-env -f <path>` and returns its output. The output contains
// all the environment variables and bash functions required to create a nix shell.
func (*Nix) PrintDevEnv(ctx context.Context, args *PrintDevEnvArgs) (*PrintDevEnvOut, error) {
	defer debug.FunctionTimer().End()
	defer trace.StartRegion(ctx, "nixPrintDevEnv").End()

	var data []byte
	var err error
	var out PrintDevEnvOut

	if args.UsePrintDevEnvCache {
		data, err = os.ReadFile(args.PrintDevEnvCachePath)
		if err == nil {
			if err := json.Unmarshal(data, &out); err != nil {
				return nil, errors.WithStack(err)
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, errors.WithStack(err)
		}
	}

	flakeDirResolved, err := filepath.EvalSymlinks(args.FlakeDir)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	if len(data) == 0 {
		cmd := exec.CommandContext(
			ctx,
			"nix", "print-dev-env",
			"path:"+flakeDirResolved,
		)
		cmd.Args = append(cmd.Args, ExperimentalFlags()...)
		cmd.Args = append(cmd.Args, "--json")
		debug.Log("Running print-dev-env cmd: %s\n", cmd)
		data, err = cmd.Output()
		if insecure, insecureErr := IsExitErrorInsecurePackage(err, "" /*pkgName*/, "" /*installable*/); insecure {
			return nil, insecureErr
		} else if err != nil {
			return nil, redact.Errorf("nix print-dev-env --json \"path:%s\": %w", flakeDirResolved, err)
		}

		if err := json.Unmarshal(data, &out); err != nil {
			return nil, redact.Errorf("unmarshal nix print-dev-env output: %w", redact.Safe(err))
		}

		if err = savePrintDevEnvCache(args.PrintDevEnvCachePath, out); err != nil {
			return nil, redact.Errorf("savePrintDevEnvCache: %w", redact.Safe(err))
		}
	}

	return &out, nil
}

func savePrintDevEnvCache(path string, out PrintDevEnvOut) error {
	data, err := json.Marshal(out)
	if err != nil {
		return errors.WithStack(err)
	}

	_ = os.WriteFile(path, data, 0o644)
	return nil
}

// FlakeNixpkgs returns a flakes-compatible reference to the nixpkgs registry.
// TODO savil. Ensure this works with the nixed cache service.
func FlakeNixpkgs(commit string) string {
	// Using nixpkgs/<commit> means:
	// The nixpkgs entry in the flake registry, with its Git revision overridden to a specific value.
	return "nixpkgs/" + commit
}

func ExperimentalFlags() []string {
	options := []string{"nix-command", "flakes"}
	if featureflag.RemoveNixpkgs.Enabled() {
		options = append(options, "fetch-closure")
	}
	return []string{
		"--extra-experimental-features", "ca-derivations",
		"--option", "experimental-features", strings.Join(options, " "),
	}
}

func System() string {
	if cachedSystem == "" {
		// While this should have been initialized, we do a best-effort to avoid
		// a panic.
		if err := ComputeSystem(); err != nil {
			panic(fmt.Sprintf(
				"System called before being initialized by ComputeSystem: %v",
				err,
			))
		}
	}
	return cachedSystem
}

var cachedSystem string

func ComputeSystem() error {
	// For Savil to debug "remove nixpkgs" feature. The Search api lacks x86-darwin info.
	// So, I need to fake that I am x86-linux and inspect the output in generated devbox.lock
	// and flake.nix files.
	// This is also used by unit tests.
	if cachedSystem != "" {
		return nil
	}
	override := os.Getenv("__DEVBOX_NIX_SYSTEM")
	if override != "" {
		cachedSystem = override
	} else {
		cmd := exec.Command(
			"nix", "eval", "--impure", "--raw", "--expr", "builtins.currentSystem",
		)
		cmd.Args = append(cmd.Args, ExperimentalFlags()...)
		out, err := cmd.Output()
		if err != nil {
			return err
		}
		cachedSystem = string(out)
	}
	return nil
}

func SystemIsLinux() bool {
	return strings.Contains(System(), "linux")
}

// version is the cached output of `nix --version`.
var version = ""

// Version returns the version of nix from `nix --version`. Usually in a semver
// like format, but not strictly.
func Version() (string, error) {
	if version != "" {
		return version, nil
	}

	cmd := command("--version")
	outBytes, err := cmd.Output()
	if err != nil {
		return "", redact.Errorf("nix command: %s", redact.Safe(cmd))
	}
	out := string(outBytes)
	const prefix = "nix (Nix) "
	if !strings.HasPrefix(out, prefix) {
		return "", redact.Errorf(`nix command %s: expected %q prefix, but output was: %s`,
			redact.Safe(cmd), redact.Safe(prefix), redact.Safe(out))
	}
	version = strings.TrimSpace(strings.TrimPrefix(out, prefix))
	return version, nil
}

var nixPlatforms = []string{
	"aarch64-darwin",
	"aarch64-linux",
	"i686-linux",
	"x86_64-darwin",
	"x86_64-linux",
	// not technically supported, but should work?
	// ref. https://nixos.wiki/wiki/Nix_on_ARM
	// ref. https://github.com/jetpack-io/devbox/pull/1300
	"armv7l-linux",
}

// EnsureValidPlatform returns an error if the platform is not supported by nix.
// https://nixos.org/manual/nix/stable/installation/supported-platforms.html
func EnsureValidPlatform(platforms ...string) error {
	ensureValid := func(platform string) error {
		for _, p := range nixPlatforms {
			if p == platform {
				return nil
			}
		}
		return usererr.New("Unsupported platform: %s. Valid platforms are: %v", platform, nixPlatforms)
	}

	for _, p := range platforms {
		if err := ensureValid(p); err != nil {
			return err
		}
	}
	return nil
}

// Warning: be careful using the bins in default/bin, they won't always match bins
// produced by the flakes.nix. Use devbox.NixBins() instead.
func ProfileBinPath(projectDir string) string {
	return filepath.Join(projectDir, ProfilePath, "bin")
}

func IsExitErrorInsecurePackage(err error, pkgNameOrEmpty, installableOrEmpty string) (bool, error) {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		if strings.Contains(string(exitErr.Stderr), "is marked as insecure") {
			packageRegex := regexp.MustCompile(`Package ([^ ]+)`)
			packageMatch := packageRegex.FindStringSubmatch(string(exitErr.Stderr))

			knownVulnerabilities := []string{}
			if installableOrEmpty != "" {
				knownVulnerabilities = PackageKnownVulnerabilities(installableOrEmpty)
			}

			insecurePackages := parseInsecurePackagesFromExitError(string(exitErr.Stderr))

			// Construct the error message.
			errMessages := []string{}
			errMessages = append(errMessages, fmt.Sprintf("Package %s is insecure.", packageMatch[1]))
			if len(knownVulnerabilities) > 0 {
				errMessages = append(errMessages,
					fmt.Sprintf("Known vulnerabilities:\n%s", strings.Join(knownVulnerabilities, "\n")))
			}
			pkgName := pkgNameOrEmpty
			if pkgName == "" {
				pkgName = "<pkg>"
			}
			errMessages = append(errMessages,
				fmt.Sprintf("To override, use `devbox add %s --allow-insecure=%s`", pkgName, strings.Join(insecurePackages, ", ")))

			return true, usererr.New(strings.Join(errMessages, "\n\n"))
		}
	}
	return false, nil
}

func parseInsecurePackagesFromExitError(errorMsg string) []string {
	insecurePackages := []string{}

	// permittedRegex is designed to match the following:
	// permittedInsecurePackages = [
	//    "package-one"
	//    "package-two"
	// ];
	permittedRegex := regexp.MustCompile(`permittedInsecurePackages\s*=\s*\[([\s\S]*?)\]`)
	permittedMatch := permittedRegex.FindStringSubmatch(errorMsg)
	if len(permittedMatch) > 1 {
		packagesList := permittedMatch[1]
		// pick out the package name strings inside the quotes
		packageMatches := regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(packagesList, -1)

		// Extract the insecure package names from the matches
		for _, packageMatch := range packageMatches {
			if len(packageMatch) > 1 {
				insecurePackages = append(insecurePackages, packageMatch[1])
			}
		}
	}

	return insecurePackages
}

func IsUserTrusted(ctx context.Context) bool {
	cmd := commandContext(ctx, "show-config", "--json")
	out, err := cmd.Output()
	if err != nil {
		return false
	}

	var config struct {
		TrustedUsers struct {
			Value []string `json:"value"`
		} `json:"trusted-users"`
	}
	if err := json.Unmarshal(out, &config); err != nil {
		return false
	}

	u, err := user.Current()
	if err != nil {
		return false
	}

	return slices.Contains(config.TrustedUsers.Value, u.Username)
}
