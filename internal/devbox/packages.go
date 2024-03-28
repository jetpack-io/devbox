// Copyright 2023 Jetpack Technologies Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package devbox

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/trace"
	"slices"
	"strings"

	"github.com/fatih/color"
	"github.com/pkg/errors"
	"github.com/samber/lo"
	"go.jetpack.io/devbox/internal/devbox/devopt"
	"go.jetpack.io/devbox/internal/devconfig"
	"go.jetpack.io/devbox/internal/devpkg"
	"go.jetpack.io/devbox/internal/devpkg/pkgtype"
	"go.jetpack.io/devbox/internal/lock"
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
		d.cfg.PackageMutator().Add(packageNameForConfig)
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
		if err := d.cfg.PackageMutator().AddPlatforms(
			d.stderr, pkg, opts.Platforms); err != nil {
			return err
		}
		if err := d.cfg.PackageMutator().ExcludePlatforms(
			d.stderr, pkg, opts.ExcludePlatforms); err != nil {
			return err
		}
		if err := d.cfg.PackageMutator().SetDisablePlugin(
			pkg, opts.DisablePlugin); err != nil {
			return err
		}
		if err := d.cfg.PackageMutator().SetPatchGLibc(
			pkg, opts.PatchGlibc); err != nil {
			return err
		}
		if err := d.cfg.PackageMutator().SetOutputs(
			d.stderr, pkg, opts.Outputs); err != nil {
			return err
		}
		if err := d.cfg.PackageMutator().SetAllowInsecure(
			d.stderr, pkg, opts.AllowInsecure); err != nil {
			return err
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

	if len(opts.Platforms) == 0 && len(opts.ExcludePlatforms) == 0 && len(opts.Outputs) == 0 && len(opts.AllowInsecure) == 0 {
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
			d.cfg.PackageMutator().Remove(found.Raw)
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

	upToDate, err := d.lockfile.IsUpToDateAndInstalled(isFishShell())
	if err != nil {
		return err
	}

	// if mode is install or uninstall, then we need to compute some state
	// like updating the flake or installing packages locally, so must continue
	// below
	if mode == ensure {
		// if mode is ensure and we are up to date, then we can skip the rest
		if upToDate {
			return nil
		}
		ux.Finfo(d.stderr, "Ensuring packages are installed.\n")
	}

	if mode == install || mode == update || mode == ensure {
		if err := d.installPackages(ctx, mode); err != nil {
			return err
		}
	}

	recomputeState := mode == ensure || d.IsEnvEnabled()
	if recomputeState {
		if err := d.recomputeState(ctx); err != nil {
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

	return d.updateLockfile(recomputeState)
}

// updateLockfile will ensure devbox.lock is up to date with the current state of the project.update
// If recomputeState is true, then we will also update the local.lock file.
func (d *Devbox) updateLockfile(recomputeState bool) error {
	// Ensure we clean out packages that are no longer needed.
	d.lockfile.Tidy()

	// Update lockfile with new packages that are not to be installed
	for _, pkg := range d.AllPackages() {
		if err := pkg.EnsureUninstallableIsInLockfile(); err != nil {
			return err
		}
	}

	// Save the lockfile at the very end, after all other operations were successful.
	if err := d.lockfile.Save(); err != nil {
		return err
	}

	// If we are recomputing state, then we need to update the local.lock file.
	// If not, we leave the local.lock in a stale state, so that state is recomputed
	// on the next ensureStateIsUpToDate call with mode=ensure.
	if recomputeState {
		configHash, err := d.ConfigHash()
		if err != nil {
			return err
		}
		return lock.UpdateAndSaveStateHashFile(lock.UpdateStateHashFileArgs{
			ProjectDir: d.projectDir,
			ConfigHash: configHash,
			IsFish:     isFishShell(),
		})
	}
	return nil
}

// recomputeState updates the local state comprising of:
// - plugins directories
// - devbox.lock file
// - the generated flake
// - the nix-profile
func (d *Devbox) recomputeState(ctx context.Context) error {
	if err := shellgen.GenerateForPrintEnv(ctx, d); err != nil {
		return err
	}

	// TODO: should this be moved into GenerateForPrintEnv?
	// OR into a plugin.GenerateFiles() along with d.pluginManager().Create()?
	if err := plugin.RemoveInvalidSymlinks(d.projectDir); err != nil {
		return err
	}

	return d.syncNixProfileFromFlake(ctx)
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

func (d *Devbox) installPackages(ctx context.Context, mode installMode) error {
	// Create plugin directories first because packages might need them
	for _, pluginConfig := range d.Config().IncludedPluginConfigs() {
		if err := d.PluginManager().CreateFilesForConfig(pluginConfig); err != nil {
			return err
		}
	}

	if err := d.installNixPackagesToStore(ctx, mode); err != nil {
		return err
	}

	return d.InstallRunXPackages(ctx)
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

// installNixPackagesToStore will install all the packages in the nix store, if
// mode is install or update, and we're not in a devbox environment.
// This is done by running `nix build` on the flake. We do this so that the
// packages will be available in the nix store when computing the devbox environment
// and installing in the nix profile (even if offline).
func (d *Devbox) installNixPackagesToStore(ctx context.Context, mode installMode) error {
	packages, err := d.packagesToInstallInStore(ctx, mode)
	if err != nil {
		return err
	}

	stepNum := 0
	total := len(packages)
	for _, pkg := range packages {
		stepNum += 1

		stepMsg := fmt.Sprintf("[%d/%d] %s", stepNum, total, pkg)
		fmt.Fprintf(d.stderr, stepMsg+"\n")

		installables, err := pkg.Installables()
		if err != nil {
			return err
		}

		// --no-link to avoid generating the result objects
		flags := []string{"--no-link"}
		if mode == update {
			flags = append(flags, "--refresh")
		}

		nixCacheConfig, err := d.providers.NixCache.Config(ctx)
		if err != nil {
			return err
		}

		for _, installable := range installables {
			args := &nix.BuildArgs{
				AllowInsecure:    pkg.HasAllowInsecure(),
				Env:              nixCacheConfig.CredentialsEnvVars(),
				ExtraSubstituter: nixCacheConfig.URI,
				Flags:            flags,
				Writer:           d.stderr,
			}
			err = nix.Build(ctx, args, installable)
			if err != nil {
				fmt.Fprintf(d.stderr, "%s: ", stepMsg)
				color.New(color.FgRed).Fprintf(d.stderr, "Fail\n")
				return packageInstallErrorHandler(err, pkg, installable)
			}
		}

		fmt.Fprintf(d.stderr, "%s: ", stepMsg)
		color.New(color.FgGreen).Fprintf(d.stderr, "Success\n")
	}
	return err
}

func (d *Devbox) packagesToInstallInStore(ctx context.Context, mode installMode) ([]*devpkg.Package, error) {
	// First, get and prepare all the packages that must be installed in this project
	// and remove non-nix packages from the list
	packages := lo.Filter(d.InstallablePackages(), devpkg.IsNix)
	if err := devpkg.FillNarInfoCache(ctx, packages...); err != nil {
		return nil, err
	}

	// Second, check which packages are not in the nix store
	packagesToInstall := []*devpkg.Package{}
	for _, pkg := range packages {
		installables, err := pkg.Installables()
		if err != nil {
			return nil, err
		}
		for _, installable := range installables {
			if mode == update {
				packagesToInstall = append(packagesToInstall, pkg)
				continue
			}
			storePaths, err := nix.StorePathsFromInstallable(ctx, installable, pkg.HasAllowInsecure())
			if err != nil {
				return nil, packageInstallErrorHandler(err, pkg, installable)
			}
			isInStore, err := nix.StorePathsAreInStore(ctx, storePaths)
			if err != nil {
				return nil, err
			}
			if !isInStore {
				packagesToInstall = append(packagesToInstall, pkg)
			}
		}
	}

	return packagesToInstall, nil
}

// packageInstallErrorHandler checks for two kinds of errors to print custom messages for so that Devbox users
// can work around them:
// 1. Packages that cannot be installed on the current system, but may be installable on other systems.packageInstallErrorHandler
// 2. Packages marked insecure by nix
func packageInstallErrorHandler(err error, pkg *devpkg.Package, installableOrEmpty string) error {
	if err == nil {
		return nil
	}

	// Check if the user is installing a package that cannot be installed on their platform.
	// For example, glibcLocales on MacOS will give the following error:
	// flake output attribute 'legacyPackages.x86_64-darwin.glibcLocales' is not a derivation or path
	// This is because glibcLocales is only available on Linux.
	// The user should try `devbox add` again with `--exclude-platform`
	errMessage := strings.TrimSpace(err.Error())

	// Sample error from `devbox add glibcLocales` on a mac:
	// error: flake output attribute 'legacyPackages.x86_64-darwin.glibcLocales' is not a derivation or path
	maybePackageSystemCompatibilityErrorType1 := strings.Contains(errMessage, "error: flake output attribute") &&
		strings.Contains(errMessage, "is not a derivation or path")
	// Sample error from `devbox add sublime4` on a mac:
	// error: Package ‘sublimetext4-4169’ in /nix/store/nlbjx0mp83p2qzf1rkmzbgvq1wxfir81-source/pkgs/applications/editors/sublime/4/common.nix:168 is not available on the requested hostPlatform:
	//     hostPlatform.config = "x86_64-apple-darwin"
	//     package.meta.platforms = [
	//       "aarch64-linux"
	//       "x86_64-linux"
	//    ]
	maybePackageSystemCompatibilityErrorType2 := strings.Contains(errMessage, "is not available on the requested hostPlatform")

	if maybePackageSystemCompatibilityErrorType1 || maybePackageSystemCompatibilityErrorType2 {
		platform := nix.System()
		return usererr.WithUserMessage(
			err,
			"package %s cannot be installed on your platform %s.\n"+
				"If you know this package is incompatible with %[2]s, then "+
				"you could run `devbox add %[1]s --exclude-platform %[2]s` and re-try.\n"+
				"If you think this package should be compatible with %[2]s, then "+
				"it's possible this particular version is not available yet from the nix registry. "+
				"You could try `devbox add` with a different version for this package.\n\n"+
				"Underlying Error from nix is:",
			pkg.Versioned(),
			platform,
		)
	}

	if isInsecureErr, userErr := nix.IsExitErrorInsecurePackage(err, pkg.Versioned(), installableOrEmpty); isInsecureErr {
		return userErr
	}

	return usererr.WithUserMessage(err, "error installing package %s", pkg.Raw)
}

// moveAllowInsecureFromLockfile will modernize a Devbox project by moving the allow_insecure: boolean
// setting from the devbox.lock file to the corresponding package in devbox.json.
//
// NOTE: ideally, this function would be in devconfig, but it leads to an import cycle with devpkg, so
// leaving in this "top-level" devbox package where we can import devconfig, devpkg and lock.
func (d *Devbox) moveAllowInsecureFromLockfile(writer io.Writer, lockfile *lock.File, cfg *devconfig.Config) error {
	if !lockfile.HasAllowInsecurePackages() {
		return nil
	}

	insecurePackages := []string{}
	for name, pkg := range lockfile.Packages {
		if pkg.AllowInsecure {
			insecurePackages = append(insecurePackages, name)
		}
		pkg.AllowInsecure = false
	}

	// Set the devbox.json packages to allow_insecure
	for _, versionedName := range insecurePackages {
		pkg := devpkg.PackageFromStringWithDefaults(versionedName, lockfile)
		storeName, err := pkg.StoreName()
		if err != nil {
			return fmt.Errorf("failed to get package's store name for package %q with error %w", versionedName, err)
		}
		if err := cfg.PackageMutator().SetAllowInsecure(writer, versionedName, []string{storeName}); err != nil {
			return fmt.Errorf("failed to set allow_insecure in devbox.json for package %q with error %w", versionedName, err)
		}
	}

	if err := d.saveCfg(); err != nil {
		return err
	}

	// Now, clear it from the lockfile
	if err := lockfile.Save(); err != nil {
		return err
	}

	ux.Finfo(
		writer,
		"Modernized the allow_insecure setting for package %q by moving it from devbox.lock to devbox.json. Please commit the changes.\n",
		strings.Join(insecurePackages, ", "),
	)

	return nil
}
