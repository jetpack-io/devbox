// Copyright 2023 Jetpack Technologies Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package impl

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/trace"
	"strings"

	"github.com/pkg/errors"
	"github.com/samber/lo"
	"go.jetpack.io/devbox/internal/devpkg"
	"go.jetpack.io/devbox/internal/nix/nixprofile"
	"go.jetpack.io/devbox/internal/shellgen"
	"golang.org/x/exp/slices"

	"go.jetpack.io/devbox/internal/boxcli/usererr"
	"go.jetpack.io/devbox/internal/debug"
	"go.jetpack.io/devbox/internal/lock"
	"go.jetpack.io/devbox/internal/nix"
	"go.jetpack.io/devbox/internal/plugin"
	"go.jetpack.io/devbox/internal/ux"
	"go.jetpack.io/devbox/internal/wrapnix"
)

// packages.go has functions for adding, removing and getting info about nix
// packages

// Add adds the `pkgs` to the config (i.e. devbox.json) and nix profile for this
// devbox project
func (d *Devbox) Add(ctx context.Context, pkgsNames ...string) error {
	ctx, task := trace.NewTask(ctx, "devboxAdd")
	defer task.End()

	// Only add packages that are not already in config. If same canonical exists,
	// replace it.
	pkgs := devpkg.PackageFromStrings(lo.Uniq(pkgsNames), d.lockfile)

	// addedPackageNames keeps track of the possibly transformed (versioned)
	// names of added packages (even if they are already in config). We use this
	// to know the exact name to mark as allowed insecure later on.
	addedPackageNames := []string{}
	for _, pkg := range pkgs {
		// If exact versioned package is already in the config, skip.
		if slices.Contains(d.cfg.Packages, pkg.Versioned()) {
			addedPackageNames = append(addedPackageNames, pkg.Versioned())
			continue
		}

		// On the other hand, if there's a package with same canonical name, replace
		// it. Ignore error (which is either missing or more than one). We search by
		// CanonicalName so any legacy or versioned packages will be removed if they
		// match.
		if name, _ := d.findPackageByName(pkg.CanonicalName()); name != "" {
			if err := d.Remove(ctx, name); err != nil {
				return err
			}
		}

		// validate that the versioned package exists in the search endpoint.
		// if not, fallback to legacy vanilla nix.
		versionedPkg := devpkg.PackageFromString(pkg.Versioned(), d.lockfile)

		packageNameForConfig := pkg.Raw
		if ok, err := versionedPkg.ValidateExists(); err == nil && ok {
			// Only use versioned if it exists in search.
			packageNameForConfig = pkg.Versioned()
		} else if !versionedPkg.IsDevboxPackage() {
			// This means it didn't validate and we don't want to fallback to legacy
			// Just propagate the error.
			return err
		} else if _, err := nix.Search(d.lockfile.LegacyNixpkgsPath(pkg.Raw)); err != nil {
			// This means it looked like a devbox package or attribute path, but we
			// could not find it in search or in the legacy nixpkgs path.
			return usererr.New("Package %s not found", pkg.Raw)
		}

		d.cfg.Packages = append(d.cfg.Packages, packageNameForConfig)
		addedPackageNames = append(addedPackageNames, packageNameForConfig)
	}

	// Resolving here ensures we allow insecure before running ensurePackagesAreInstalled
	// which will call print-dev-env. Resolving does not save the lockfile, we
	// save at the end when everything has succeeded.
	if d.allowInsecureAdds {
		for _, name := range addedPackageNames {
			p, err := d.lockfile.Resolve(name)
			if err != nil {
				return err
			}
			p.AllowInsecure = true
		}
	}

	if err := d.ensurePackagesAreInstalled(ctx, install); err != nil {
		return usererr.WithUserMessage(err, "There was an error installing nix packages")
	}

	if err := d.saveCfg(); err != nil {
		return err
	}

	for _, input := range pkgs {
		if err := plugin.PrintReadme(
			ctx,
			input,
			d.projectDir,
			d.writer,
			false /*markdown*/); err != nil {
			return err
		}
	}

	if err := d.lockfile.Save(); err != nil {
		return err
	}

	return wrapnix.CreateWrappers(ctx, d)
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
		if found != "" {
			packagesToUninstall = append(packagesToUninstall, found)
			d.cfg.Packages = lo.Without(d.cfg.Packages, found)
		} else {
			missingPkgs = append(missingPkgs, pkg)
		}
	}

	if len(missingPkgs) > 0 {
		ux.Fwarning(
			d.writer,
			"the following packages were not found in your devbox.json: %s\n",
			strings.Join(missingPkgs, ", "),
		)
	}

	if err := plugin.Remove(d.projectDir, packagesToUninstall); err != nil {
		return err
	}

	if err := d.removePackagesFromProfile(ctx, packagesToUninstall); err != nil {
		return err
	}

	if err := d.ensurePackagesAreInstalled(ctx, uninstall); err != nil {
		return err
	}

	if err := d.lockfile.Remove(packagesToUninstall...); err != nil {
		return err
	}

	if err := d.saveCfg(); err != nil {
		return err
	}

	return wrapnix.CreateWrappers(ctx, d)
}

// installMode is an enum for helping with ensurePackagesAreInstalled implementation
type installMode string

const (
	install   installMode = "install"
	uninstall installMode = "uninstall"
	ensure    installMode = "ensure"
)

// ensurePackagesAreInstalled ensures that the nix profile has the packages specified
// in the config (devbox.json). The `mode` is used for user messaging to explain
// what operations are happening, because this function may take time to execute.
func (d *Devbox) ensurePackagesAreInstalled(ctx context.Context, mode installMode) error {
	defer trace.StartRegion(ctx, "ensurePackages").End()

	localLock, err := lock.Local(d)
	if err != nil {
		return err
	}

	upToDate, err := localLock.IsUpToDate()
	if err != nil {
		return err
	}
	if upToDate {
		return nil
	}

	if mode == ensure {
		fmt.Fprintln(d.writer, "Ensuring packages are installed.")
	}

	if err := d.syncPackagesToProfile(ctx, mode); err != nil {
		return err
	}

	if err := shellgen.GenerateForPrintEnv(ctx, d); err != nil {
		return err
	}

	if err := plugin.RemoveInvalidSymlinks(d.projectDir); err != nil {
		return err
	}

	// Force print-dev-env cache to be recomputed.
	if _, err = d.computeNixEnv(ctx, false /*use cache*/); err != nil {
		return err
	}

	// Ensure we clean out packages that are no longer needed.
	d.lockfile.Tidy()

	if err = d.lockfile.Save(); err != nil {
		return err
	}

	return localLock.Update()
}

func (d *Devbox) profilePath() (string, error) {
	absPath := filepath.Join(d.projectDir, nix.ProfilePath)

	if err := resetProfileDirForFlakes(absPath); err != nil {
		debug.Log("ERROR: resetProfileDirForFlakes error: %v\n", err)
	}

	return absPath, errors.WithStack(os.MkdirAll(filepath.Dir(absPath), 0o755))
}

// syncPackagesToProfile ensures that all packages in devbox.json exist in the nix profile,
// and no more.
func (d *Devbox) syncPackagesToProfile(ctx context.Context, mode installMode) error {
	// TODO: we can probably merge these two operations to be faster and minimize chances of
	// the devbox.json and nix profile falling out of sync.
	if err := d.addPackagesToProfile(ctx, mode); err != nil {
		return err
	}

	return d.tidyProfile(ctx)
}

// addPackagesToProfile inspects the packages in devbox.json, checks which of them
// are missing from the nix profile, and then installs each package individually into the
// nix profile.
func (d *Devbox) addPackagesToProfile(ctx context.Context, mode installMode) error {
	defer trace.StartRegion(ctx, "addNixProfilePkgs").End()

	if mode == uninstall {
		return nil
	}

	// If packages are in profile but nixpkgs has been purged, the experience
	// will be poor when we try to run print-dev-env. So we ensure nixpkgs is
	// prefetched for all relevant packages (those not in binary cache).
	for _, input := range d.PackagesAsInputs() {
		if err := input.EnsureNixpkgsPrefetched(d.writer); err != nil {
			return err
		}
	}

	pkgs, err := d.pendingPackagesForInstallation(ctx)
	if err != nil {
		return err
	}

	if len(pkgs) == 0 {
		return nil
	}

	var msg string
	if len(pkgs) == 1 {
		msg = fmt.Sprintf("Installing package: %s.", pkgs[0])
	} else {
		msg = fmt.Sprintf("Installing %d packages: %s.", len(pkgs), strings.Join(pkgs, ", "))
	}
	fmt.Fprintf(d.writer, "\n%s\n\n", msg)

	profileDir, err := d.profilePath()
	if err != nil {
		return err
	}

	total := len(pkgs)
	for idx, pkg := range pkgs {
		stepNum := idx + 1

		stepMsg := fmt.Sprintf("[%d/%d] %s", stepNum, total, pkg)

		if err := nixprofile.ProfileInstall(&nixprofile.ProfileInstallArgs{
			CustomStepMessage: stepMsg,
			Lockfile:          d.lockfile,
			Package:           pkg,
			ProfilePath:       profileDir,
			Writer:            d.writer,
		}); err != nil {
			return err
		}
	}

	return nil
}

func (d *Devbox) removePackagesFromProfile(ctx context.Context, pkgs []string) error {
	defer trace.StartRegion(ctx, "removeNixProfilePkgs").End()

	profileDir, err := d.profilePath()
	if err != nil {
		return err
	}

	for _, input := range devpkg.PackageFromStrings(pkgs, d.lockfile) {
		index, err := nixprofile.ProfileListIndex(&nixprofile.ProfileListIndexArgs{
			Lockfile:   d.lockfile,
			Writer:     d.writer,
			Input:      input,
			ProfileDir: profileDir,
		})
		if err != nil {
			ux.Ferror(
				d.writer,
				"Package %s not found in profile. Skipping.\n",
				input.Raw,
			)
			continue
		}

		// TODO: unify this with nix.ProfileRemove
		cmd := nix.Command(ctx, "profile", "remove",
			"--profile", profileDir,
			fmt.Sprintf("%d", index),
		)
		cmd.Stdout = d.writer
		cmd.Stderr = d.writer
		err = cmd.Run()
		if err != nil {
			return err
		}
	}
	return nil
}

// tidyProfile removes any packages in the nix profile that are not in devbox.json.
func (d *Devbox) tidyProfile(ctx context.Context) error {
	defer trace.StartRegion(ctx, "tidyProfile").End()

	extras, err := d.extraPackagesInProfile(ctx)
	if err != nil {
		return err
	}

	profileDir, err := d.profilePath()
	if err != nil {
		return err
	}

	// Remove by index to avoid comparing nix.ProfileListItem <> nix.Inputs again.
	return nixprofile.ProfileRemoveItems(profileDir, extras)
}

// pendingPackagesForInstallation returns a list of packages that are in
// devbox.json or global devbox.json but are not yet installed in the nix
// profile. It maintains the order of packages as specified by
// Devbox.packages() (higher priority first)
func (d *Devbox) pendingPackagesForInstallation(ctx context.Context) ([]string, error) {
	defer trace.StartRegion(ctx, "pendingPackages").End()

	profileDir, err := d.profilePath()
	if err != nil {
		return nil, err
	}

	pending := []string{}
	list, err := nixprofile.ProfileListItems(d.writer, profileDir)
	if err != nil {
		return nil, err
	}
	for _, input := range d.PackagesAsInputs() {
		_, err := nixprofile.ProfileListIndex(&nixprofile.ProfileListIndexArgs{
			List:       list,
			Lockfile:   d.lockfile,
			Writer:     d.writer,
			Input:      input,
			ProfileDir: profileDir,
		})
		if err != nil {
			if !errors.Is(err, nix.ErrPackageNotFound) {
				return nil, err
			}
			pending = append(pending, input.Raw)
		}
	}
	return pending, nil
}

// extraPkgsInProfile returns a list of packages that are in the nix profile,
// but are NOT in devbox.json or global devbox.json.
//
// NOTE: as an optimization, this implementation assumes that all packages in
// devbox.json have already been added to the nix profile.
func (d *Devbox) extraPackagesInProfile(ctx context.Context) ([]*nixprofile.NixProfileListItem, error) {
	defer trace.StartRegion(ctx, "extraPackagesInProfile").End()

	profileDir, err := d.profilePath()
	if err != nil {
		return nil, err
	}

	profileItems, err := nixprofile.ProfileListItems(d.writer, profileDir)
	if err != nil {
		return nil, err
	}
	devboxInputs := d.PackagesAsInputs()

	if len(devboxInputs) == len(profileItems) {
		// Optimization: skip comparison if number of packages are the same. This only works
		// because we assume that all packages in `devbox.json` have just been added to the
		// profile.
		return nil, nil
	}

	extras := []*nixprofile.NixProfileListItem{}
	// Note: because nix.Input uses memoization when normalizing attribute paths (slow operation),
	// and since we're reusing the Input objects, this O(n*m) loop becomes O(n+m) wrt the slow operation.
outer:
	for _, item := range profileItems {
		profileInput := item.ToPackage(d.lockfile)
		for _, devboxInput := range devboxInputs {
			if profileInput.Equals(devboxInput) {
				continue outer
			}
		}
		extras = append(extras, item)
	}

	return extras, nil
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
