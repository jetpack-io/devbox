// Copyright 2024 Jetify Inc. and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package nix

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/trace"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"go.jetpack.io/devbox/internal/boxcli/featureflag"
	"go.jetpack.io/devbox/internal/boxcli/usererr"
	"go.jetpack.io/devbox/internal/redact"
	"golang.org/x/mod/semver"

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

// All major Nix versions supported by Devbox.
const (
	Version2_12 = "2.12.0"
	Version2_13 = "2.13.0"
	Version2_14 = "2.14.0"
	Version2_15 = "2.15.0"
	Version2_16 = "2.16.0"
	Version2_17 = "2.17.0"
	Version2_18 = "2.18.0"
	Version2_19 = "2.19.0"
	Version2_20 = "2.20.0"
	Version2_21 = "2.21.0"

	MinVersion = Version2_12
)

// versionRegexp matches the first line of "nix --version" output.
// Semantic component sourced from https://semver.org/#is-there-a-suggested-regular-expression-regex-to-check-a-semver-string
var versionRegexp = regexp.MustCompile(`^(.+) \(.+\) ((?P<major>0|[1-9]\d*)\.(?P<minor>0|[1-9]\d*)\.(?P<patch>0|[1-9]\d*)(?:-(?P<prerelease>(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+(?P<buildmetadata>[0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?)$`)

// VersionInfo contains information about a Nix installation.
type VersionInfo struct {
	// Name is the executed program name (the first element of argv).
	Name string

	// Version is the semantic Nix version string.
	Version string

	// System is the current Nix system. It follows the pattern <arch>-<os>
	// and does not use the same values as GOOS or GOARCH.
	System string

	// ExtraSystems are other systems that the current machine supports.
	// Usually set by the extra-platforms setting in nix.conf.
	ExtraSystems []string

	// Features are the capabilities that the Nix binary was compiled with.
	Features []string

	// SystemConfig is the path to the Nix system configuration file,
	// usually /etc/nix/nix.conf.
	SystemConfig string

	// UserConfigs is a list of paths to the user's Nix configuration files.
	UserConfigs []string

	// StoreDir is the path to the Nix store directory, usually /nix/store.
	StoreDir string

	// StateDir is the path to the Nix state directory, usually
	// /nix/var/nix.
	StateDir string

	// DataDir is the path to the Nix data directory, usually somewhere
	// within the Nix store. This field is empty for Nix versions <= 2.12.
	DataDir string
}

func parseVersionInfo(data []byte) (VersionInfo, error) {
	// Example nix --version --debug output from Nix versions 2.12 to 2.21.
	// Version 2.12 omits the data directory, but they're otherwise
	// identical.
	//
	// See https://github.com/NixOS/nix/blob/5b9cb8b3722b85191ee8cce8f0993170e0fc234c/src/libmain/shared.cc#L284-L305
	//
	// nix (Nix) 2.21.2
	// System type: aarch64-darwin
	// Additional system types: x86_64-darwin
	// Features: gc, signed-caches
	// System configuration file: /etc/nix/nix.conf
	// User configuration files: /Users/nobody/.config/nix/nix.conf:/etc/xdg/nix/nix.conf
	// Store directory: /nix/store
	// State directory: /nix/var/nix
	// Data directory: /nix/store/m0ns07v8by0458yp6k30rfq1rs3kaz6g-nix-2.21.2/share

	info := VersionInfo{}
	if len(data) == 0 {
		return info, redact.Errorf("empty nix --version output")
	}

	lines := strings.Split(string(data), "\n")
	matches := versionRegexp.FindStringSubmatch(lines[0])
	if len(matches) < 3 {
		return info, redact.Errorf("parse nix version: %s", redact.Safe(lines[0]))
	}
	info.Name = matches[1]
	info.Version = matches[2]
	for _, line := range lines {
		name, value, found := strings.Cut(line, ": ")
		if !found {
			continue
		}

		switch name {
		case "System type":
			info.System = value
		case "Additional system types":
			info.ExtraSystems = strings.Split(value, ", ")
		case "Features":
			info.Features = strings.Split(value, ", ")
		case "System configuration file":
			info.SystemConfig = value
		case "User configuration files":
			info.UserConfigs = strings.Split(value, ":")
		case "Store directory":
			info.StoreDir = value
		case "State directory":
			info.StateDir = value
		case "Data directory":
			info.DataDir = value
		}
	}
	return info, nil
}

// AtLeast returns true if v.Version is >= version per semantic versioning. It
// always returns false if v.Version is empty or invalid, such as when the
// current Nix version cannot be parsed. It panics if version is an invalid
// semver.
func (v VersionInfo) AtLeast(version string) bool {
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	if !semver.IsValid(version) {
		panic(fmt.Sprintf("nix.atLeast: invalid version %q", version[1:]))
	}
	return semver.Compare("v"+v.Version, version) >= 0
}

// version is the cached output of `nix --version --debug`.
var versionInfo = sync.OnceValues(runNixVersion)

func runNixVersion() (VersionInfo, error) {
	// Arbitrary timeout to make sure we don't take too long or hang.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Intentionally don't use the nix.command function here. We use this to
	// perform Nix version checks and don't want to pass any extra-features
	// or flags that might be missing from old versions.
	cmd := exec.CommandContext(ctx, "nix", "--version", "--debug")
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) != 0 {
			return VersionInfo{}, redact.Errorf("nix command: %s: %q: %v", redact.Safe(cmd), exitErr.Stderr, err)
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return VersionInfo{}, redact.Errorf("nix command: %s: timed out while reading output: %v", redact.Safe(cmd), err)
		}
		return VersionInfo{}, redact.Errorf("nix command: %s: %v", redact.Safe(cmd), err)
	}

	debug.Log("nix --version --debug output:\n%s", out)
	return parseVersionInfo(out)
}

// Version returns the currently installed version of Nix.
func Version() (VersionInfo, error) {
	return versionInfo()
}

var nixPlatforms = []string{
	"aarch64-darwin",
	"aarch64-linux",
	"i686-linux",
	"x86_64-darwin",
	"x86_64-linux",
	// not technically supported, but should work?
	// ref. https://nixos.wiki/wiki/Nix_on_ARM
	// ref. https://github.com/jetify-com/devbox/pull/1300
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

var ErrUnknownServiceManager = errors.New("unknown service manager")

func restartDaemon(ctx context.Context) error {
	if runtime.GOOS != "darwin" {
		err := fmt.Errorf("don't know how to restart nix daemon: %w", ErrUnknownServiceManager)
		return &DaemonError{err: err}
	}

	cmd := exec.CommandContext(ctx, "launchctl", "bootout", "system", "/Library/LaunchDaemons/org.nixos.nix-daemon.plist")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return &DaemonError{
			cmd:    cmd.String(),
			stderr: out,
			err:    fmt.Errorf("stop nix daemon: %w", err),
		}
	}
	cmd = exec.CommandContext(ctx, "launchctl", "bootstrap", "system", "/Library/LaunchDaemons/org.nixos.nix-daemon.plist")
	out, err = cmd.CombinedOutput()
	if err != nil {
		return &DaemonError{
			cmd:    cmd.String(),
			stderr: out,
			err:    fmt.Errorf("start nix daemon: %w", err),
		}
	}

	// TODO(gcurtis): poll for daemon to come back instead.
	time.Sleep(2 * time.Second)
	return nil
}
