// Copyright 2023 Jetpack Technologies Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package devbox

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/trace"
	"slices"
	"strings"

	"github.com/pkg/errors"
	"github.com/samber/lo"
	"go.jetpack.io/devbox/internal/devbox/devopt"
	"go.jetpack.io/devbox/internal/devpkg"
	"go.jetpack.io/devbox/internal/devpkg/pkgtype"
	"go.jetpack.io/devbox/internal/shellgen"

	"go.jetpack.io/devbox/internal/boxcli/usererr"
	"go.jetpack.io/devbox/internal/debug"
	"go.jetpack.io/devbox/internal/nix"
	"go.jetpack.io/devbox/internal/plugin"
	"go.jetpack.io/devbox/internal/ux"
)

// packages.go has functions for adding, removing and getting info about nix
// packages

// Add adds the `pkgs` to the config (i.e. devbox.json) and nix profile for this
// devbox project
func (d *Devbox) Add(ctx context.Context, pkgsNames []string, opts devopt.AddOpts) error {
	ctx, task := trace.NewTask(ctx, "devboxAdd")
	defer task.End()

	// Track which packages had no changes so we can report that to the user.
	unchangedPackageNames := []string{}

	// Only add packages that are not already in config. If same canonical exists,
	// replace it.
	pkgs := devpkg.PackagesFromStringsWithOptions(lo.Uniq(pkgsNames), d.lockfile, opts)

	// addedPackageNames keeps track of the possibly transformed (versioned)
	// names of added packages (even if they are already in config). We use this
	// to know the exact name to mark as allowed insecure later on.
	addedPackageNames := []string{}
	existingPackageNames := d.PackageNames()
	for _, pkg := range pkgs {
		// If exact versioned package is already in the config, we can skip the
		// next loop that only deals with newPackages.
		if slices.Contains(existingPackageNames, pkg.Versioned()) {
			// But we still need to add to addedPackageNames. See its comment.
			addedPackageNames = append(addedPackageNames, pkg.Versioned())
			unchangedPackageNames = append(unchangedPackageNames, pkg.Versioned())
			ux.Finfo(d.stderr, "Package %q already in devbox.json\n", pkg.Versioned())
			continue
		}

		// On the other hand, if there's a package with same canonical name, replace
		// it. Ignore error (which is either missing or more than one). We search by
		// CanonicalName so any legacy or versioned packages will be removed if they
		// match.
		found, _ := d.findPackageByName(pkg.CanonicalName())
		if found != nil {
			ux.Finfo(d.stderr, "Replacing package %q in devbox.json\n", found.Raw)
			if err := d.Remove(ctx, found.Raw); err != nil {
				return err
			}
		}

		// validate that the versioned package exists in the search endpoint.
		// if not, fallback to legacy vanilla nix.
		versionedPkg := devpkg.PackageFromStringWithOptions(pkg.Versioned(), d.lockfile, opts)

		packageNameForConfig := pkg.Raw
		ok, err := versionedPkg.ValidateExists(ctx)
		if (err == nil && ok) || errors.Is(err, devpkg.ErrCannotBuildPackageOnSystem) {
			// Only use versioned if it exists in search. We can disregard the error
			// about not building on the current system, since user's can continue
			// via --exclude-platform flag.
			packageNameForConfig = pkg.Versioned()
		} else if !versionedPkg.IsDevboxPackage {
			// This means it didn't validate and we don't want to fallback to legacy
			// Just propagate the error.
			return err
		} else if _, err := nix.Search(d.lockfile.LegacyNixpkgsPath(pkg.Raw)); err != nil {
			// This means it looked like a devbox package or attribute path, but we
			// could not find it in search or in the legacy nixpkgs path.
			return usererr.New("Package %s not found", pkg.Raw)
		}

		ux.Finfo(d.stderr, "Adding package %q to devbox.json\n", packageNameForConfig)
		d.cfg.Packages.Add(packageNameForConfig)
		addedPackageNames = append(addedPackageNames, packageNameForConfig)
	}

	// Options must be set before ensureStateIsUpToDate. See comment in function
	if err := d.setPackageOptions(addedPackageNames, opts); err != nil {
		return err
	}

	if err := d.ensureStateIsUpToDate(ctx, install); err != nil {
		return usererr.WithUserMessage(err, "There was an error installing nix packages")
	}

	if err := d.saveCfg(); err != nil {
		return err
	}

	return d.printPostAddMessage(ctx, pkgs, unchangedPackageNames, opts)
}

func (d *Devbox) setPackageOptions(pkgs []string, opts devopt.AddOpts) error {
	for _, pkg := range pkgs {
		if err := d.cfg.Packages.AddPlatforms(
			d.stderr, pkg, opts.Platforms); err != nil {
			return err
		}
		if err := d.cfg.Packages.ExcludePlatforms(
			d.stderr, pkg, opts.ExcludePlatforms); err != nil {
			return err
		}
		if err := d.cfg.Packages.SetDisablePlugin(
			pkg, opts.DisablePlugin); err != nil {
			return err
		}

		if err := d.cfg.Packages.SetPatchGLibc(
			pkg, opts.PatchGlibc); err != nil {
			return err
		}
	}

	// Resolving here ensures we allow insecure before running ensureStateIsUpToDate
	// which will call print-dev-env. Resolving does not save the lockfile, we
	// save at the end when everything has succeeded.
	if opts.AllowInsecure {
		nix.AllowInsecurePackages()
		for _, name := range pkgs {
			p, err := d.lockfile.Resolve(name)
			if err != nil {
				return err
			}
			// TODO: Now that config packages can have fields,
			// we should set this in the config, not the lockfile.
			if !p.AllowInsecure {
				fmt.Fprintf(d.stderr, "Allowing insecure for %s\n", name)
			}
			p.AllowInsecure = true
		}
	}

	return nil
}

func (d *Devbox) printPostAddMessage(
	ctx context.Context,
	pkgs []*devpkg.Package,
	unchangedPackageNames []string,
	opts devopt.AddOpts,
) error {
	for _, input := range pkgs {
		if readme, err := plugin.Readme(
			ctx,
			input,
			d.projectDir,
			false /*markdown*/); err != nil {
			return err
		} else if readme != "" {
			fmt.Fprintf(d.stderr, "%s\n", readme)
		}
	}

	if len(opts.Platforms) == 0 && len(opts.ExcludePlatforms) == 0 && !opts.AllowInsecure {
		if len(unchangedPackageNames) == 1 {
			ux.Finfo(d.stderr, "Package %q was already in devbox.json and was not modified\n", unchangedPackageNames[0])
		} else if len(unchangedPackageNames) > 1 {
			ux.Finfo(d.stderr, "Packages %s were already in devbox.json and were not modified\n",
				strings.Join(unchangedPackageNames, ", "),
			)
		}
	}
	return nil
}

// Remove removes the `pkgs` from the config (i.e. devbox.json) and nix profile
// for this devbox project
func (d *Devbox) Remove(ctx context.Context, pkgs ...string) error {
	ctx, task := trace.NewTask(ctx, "devboxRemove")
	defer task.End()

	packagesToUninstall := []string{}
	missingPkgs := []string{}
	for _, pkg := range lo.Uniq(pkgs) {
		found, _ := d.findPackageByName(pkg)
		if found != nil {
			packagesToUninstall = append(packagesToUninstall, found.Raw)
			d.cfg.Packages.Remove(found.Raw)
		} else {
			missingPkgs = append(missingPkgs, pkg)
		}
	}

	if len(missingPkgs) > 0 {
		ux.Fwarning(
			d.stderr,
			"the following packages were not found in your devbox.json: %s\n",
			strings.Join(missingPkgs, ", "),
		)
	}

	if err := plugin.Remove(d.projectDir, packagesToUninstall); err != nil {
		return err
	}

	// this will clean up the now-extra package from nix profile and the lockfile
	if err := d.ensureStateIsUpToDate(ctx, uninstall); err != nil {
		return err
	}

	return d.saveCfg()
}

// installMode is an enum for helping with ensureStateIsUpToDate implementation
type installMode string

const (
	install   installMode = "install"
	uninstall installMode = "uninstall"
	// update is both install new package version and uninstall old package version
	update installMode = "update"
	ensure installMode = "ensure"
)

// ensureStateIsUpToDate ensures the Devbox project state is up to date.
// Namely:
//  1. Packages are installed, in nix-profile or runx.
//     Extraneous packages are removed (references purged, not uninstalled).
//  2. Plugins are installed
//  3. Files for devbox shellenv are generated
//  4. The Devbox environment is re-computed, if necessary, and cached
//  5. Lockfile is synced
//
// The `mode` is used for:
// 1. Skipping certain operations that may not apply.
// 2. User messaging to explain what operations are happening, because this function may take time to execute.
func (d *Devbox) ensureStateIsUpToDate(ctx context.Context, mode installMode) error {
	defer trace.StartRegion(ctx, "devboxEnsureStateIsUpToDate").End()
	defer debug.FunctionTimer().End()

	// if mode is install or uninstall, then we need to update the nix-profile
	// and lockfile, so we must continue below.
	upToDate, err := d.lockfile.IsUpToDateAndInstalled()
	if err != nil {
		return err
	}

	if mode == ensure {
		// if mode is ensure and we are up to date, then we can skip the rest
		if upToDate {
			return nil
		}
		fmt.Fprintln(d.stderr, "Ensuring packages are installed.")
	}

	// Validate packages. Must be run up-front and definitely prior to computeEnv
	// and syncFlakeToProfile below that will evaluate the flake and may give
	// inscrutable errors if the package is uninstallable.
	if err := d.validatePackages(); err != nil {
		return err
	}

	// Create plugin directories first because packages might need them
	for _, pkg := range d.InstallablePackages() {
		if err := d.PluginManager().Create(pkg); err != nil {
			return err
		}
	}

	if err := d.InstallRunXPackages(ctx); err != nil {
		return err
	}

	if err := shellgen.GenerateForPrintEnv(ctx, d); err != nil {
		return err
	}

	if err := plugin.RemoveInvalidSymlinks(d.projectDir); err != nil {
		return err
	}

	// Skip the print-dev-env's cache if we are in a devbox-environment. If not,
	// skip the cache if we're either installing packages or ensuring
	// the project state is up-to-date.
	skipPrintDevEnvCache := true
	// skipPrintDevEnvCache := d.IsEnvEnabled() || (mode == ensure || mode == install)
	if _, err := d.computeEnv(ctx, !skipPrintDevEnvCache /*usePrintDevEnvCache*/); err != nil {
		return err
	}

	// Ensure the nix profile has the packages from the flake.
	if err := d.syncFlakeToProfile(ctx); err != nil {
		return err
	}

	// Ensure we clean out packages that are no longer needed.
	d.lockfile.Tidy()

	// Update lockfile with new packages that are not to be installed
	for _, pkg := range d.configPackages() {
		if err := pkg.EnsureUninstallableIsInLockfile(); err != nil {
			return err
		}
	}

	// If we're in a devbox shell (global or project), then the environment might
	// be out of date after the user installs something. If have direnv active
	// it should reload automatically so we don't need to refresh.
	if d.IsEnvEnabled() && !upToDate && !d.IsDirenvActive() {
		ux.Fwarning(
			d.stderr,
			"Your shell environment may be out of date. Run `%s` to update it.\n",
			d.refreshAliasOrCommand(),
		)
	}

	return d.lockfile.Save()
}

func (d *Devbox) profilePath() (string, error) {
	absPath := filepath.Join(d.projectDir, nix.ProfilePath)

	if err := resetProfileDirForFlakes(absPath); err != nil {
		debug.Log("ERROR: resetProfileDirForFlakes error: %v\n", err)
	}

	return absPath, errors.WithStack(os.MkdirAll(filepath.Dir(absPath), 0o755))
}

var resetCheckDone = false

// resetProfileDirForFlakes ensures the profileDir directory is cleared of old
// state if the Flakes feature has been changed, from the previous execution of a devbox command.
func resetProfileDirForFlakes(profileDir string) (err error) {
	if resetCheckDone {
		return nil
	}
	defer func() {
		if err == nil {
			resetCheckDone = true
		}
	}()

	dir, err := filepath.EvalSymlinks(profileDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.WithStack(err)
	}

	// older nix profiles have a manifest.nix file present
	_, err = os.Stat(filepath.Join(dir, "manifest.nix"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.WithStack(err)
	}

	return errors.WithStack(os.Remove(profileDir))
}

func (d *Devbox) InstallRunXPackages(ctx context.Context) error {
	for _, pkg := range lo.Filter(d.InstallablePackages(), devpkg.IsRunX) {
		lockedPkg, err := d.lockfile.Resolve(pkg.Raw)
		if err != nil {
			return err
		}
		if _, err := pkgtype.RunXClient().Install(
			ctx,
			lockedPkg.Resolved,
		); err != nil {
			return fmt.Errorf("error installing runx package %s: %w", pkg, err)
		}
	}
	return nil
}

// validatePackages will ensure that packages are available to be installed
// in the user's current system.
func (d *Devbox) validatePackages() error {
	for _, pkg := range d.InstallablePackages() {

		inCache, err := pkg.IsInBinaryCache()
		if err != nil {
			return err
		}

		if !inCache && nix.IsGithubNixpkgsURL(pkg.URLForFlakeInput()) {
			if err := nix.EnsureNixpkgsPrefetched(d.stderr, pkg.HashFromNixPkgsURL()); err != nil {
				return err
			}
			if exists, err := pkg.ValidateInstallsOnSystem(); err != nil {
				return err
			} else if !exists {
				platform := nix.System()
				return usererr.New(
					"package %s cannot be installed on your platform %s.\n"+
						"If you know this package is incompatible with %[2]s, then "+
						"you could run `devbox add %[1]s --exclude-platform %[2]s` and re-try.\n"+
						"If you think this package should be compatible with %[2]s, then "+
						"it's possible this particular version is not available yet from the nix registry. "+
						"You could try `devbox add` with a different version for this package.\n",
					pkg.Raw,
					platform,
				)
			}
		}
	}
	return nil
}
