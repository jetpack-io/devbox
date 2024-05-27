// Copyright 2024 Jetify Inc. and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package devpkg

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/pkg/errors"
	"github.com/samber/lo"
	"go.jetpack.io/devbox/internal/boxcli/usererr"
	"go.jetpack.io/devbox/internal/cachehash"
	"go.jetpack.io/devbox/internal/debug"
	"go.jetpack.io/devbox/internal/devbox/devopt"
	"go.jetpack.io/devbox/internal/devconfig/configfile"
	"go.jetpack.io/devbox/internal/devpkg/pkgtype"
	"go.jetpack.io/devbox/internal/lock"
	"go.jetpack.io/devbox/internal/nix"
	"go.jetpack.io/devbox/internal/ux"
	"go.jetpack.io/devbox/nix/flake"
	"go.jetpack.io/devbox/plugins"
)

// Package represents a "package" added to the devbox.json config.
// A unique feature of flakes is that they have well-defined "inputs" and "outputs".
// This Package will be aggregated into a specific "flake input" (see shellgen.flakeInput).
type Package struct {
	plugins.BuiltIn
	lockfile        lock.Locker
	IsDevboxPackage bool

	// If package triggers a built-in plugin, setting this to true will disable it.
	// If package does not trigger plugin, this will have no effect.
	DisablePlugin bool

	// installable is the flake attribute that the package resolves to.
	// When it gets set depends on the original package string:
	//
	// - If the parsed package string is unambiguously a flake installable
	//   (not "name" or "name@version"), then it is set immediately.
	// - Otherwise, it's set after calling resolve.
	//
	// This is done for performance reasons. Some commands don't require the
	// fully-resolved package, so we don't want to waste time computing it.
	installable flake.Installable

	// resolve resolves a Devbox package string to a Nix installable.
	//
	// - If the package exists in the lockfile, it resolves to the
	//   lockfile's installable.
	// - If the package doesn't exist in the lockfile, it resolves to the
	//   installable returned by the search index (/v1/resolve).
	//
	// After resolving the installable, it also sets storePath when the
	// package exists in the Nix binary cache.
	//
	// For flake packages (non-devbox packages), resolve is a no-op.
	resolve func() error

	// Raw is the devbox package name from the devbox.json config.
	// Raw has a few forms:
	// 1. Devbox Packages
	//    a. versioned packages
	//       examples:  go@1.20, python@latest
	//    b. any others?
	// 2. Local
	//    flakes in a relative sub-directory
	//    example: ./local_flake_subdir#myPackage
	// 3. Github
	//    remote flakes with raw name starting with `Github:`
	//    example: github:nixos/nixpkgs/5233fd2ba76a3accb5aaa999c00509a11fd0793c#hello
	Raw string

	// Outputs is a list of outputs to build from the package's derivation.
	outputs outputs

	// patchGlibc applies a function to the package's derivation that
	// patches any ELF binaries to use the latest version of nixpkgs#glibc.
	// It's a function to allow deferring nix System call until it's needed.
	patchGlibc func() bool

	// AllowInsecure are a list of nix packages that are whitelisted to be
	// installed even if they are marked as insecure.
	AllowInsecure []string

	// isInstallable is true if the package may be enabled on the current platform.
	// It's a function to allow deferring nix System call until it's needed.
	isInstallable func() bool

	normalizedPackageAttributePathCache string // memoized value from normalizedPackageAttributePath()
}

func PackagesFromStringsWithOptions(rawNames []string, l lock.Locker, opts devopt.AddOpts) []*Package {
	packages := []*Package{}
	for _, name := range rawNames {
		packages = append(packages, PackageFromStringWithOptions(name, l, opts))
	}
	return packages
}

func PackagesFromConfig(packages []configfile.Package, l lock.Locker) []*Package {
	result := []*Package{}
	for _, cfgPkg := range packages {
		pkg := newPackage(cfgPkg.VersionedName(), cfgPkg.IsEnabledOnPlatform, l)
		pkg.DisablePlugin = cfgPkg.DisablePlugin
		pkg.patchGlibc = sync.OnceValue(func() bool {
			return cfgPkg.PatchGlibc && nix.SystemIsLinux()
		})
		pkg.outputs.selectedNames = lo.Uniq(append(pkg.outputs.selectedNames, cfgPkg.Outputs...))
		pkg.AllowInsecure = cfgPkg.AllowInsecure
		result = append(result, pkg)
	}
	return result
}

func PackageFromStringWithDefaults(raw string, locker lock.Locker) *Package {
	return newPackage(raw, func() bool { return true } /*isInstallable*/, locker)
}

func PackageFromStringWithOptions(raw string, locker lock.Locker, opts devopt.AddOpts) *Package {
	pkg := PackageFromStringWithDefaults(raw, locker)
	pkg.DisablePlugin = opts.DisablePlugin
	pkg.patchGlibc = sync.OnceValue(func() bool { return opts.PatchGlibc })
	pkg.outputs.selectedNames = lo.Uniq(append(pkg.outputs.selectedNames, opts.Outputs...))
	pkg.AllowInsecure = opts.AllowInsecure
	return pkg
}

func newPackage(raw string, isInstallable func() bool, locker lock.Locker) *Package {
	pkg := &Package{
		Raw:           raw,
		lockfile:      locker,
		isInstallable: sync.OnceValue(isInstallable),
	}

	// The raw string is either a Devbox package ("name" or "name@version")
	// or it's a flake installable. In some cases they're ambiguous
	// ("nixpkgs" is a devbox package and a flake). When that happens, we
	// assume a Devbox package.
	parsed, err := flake.ParseInstallable(raw)
	if err != nil || pkgtype.IsAmbiguous(raw, parsed) {
		pkg.IsDevboxPackage = true
		pkg.resolve = sync.OnceValue(func() error { return resolve(pkg) })
		return pkg
	}

	// We currently don't lock flake references in devbox.lock, so there's
	// nothing to resolve.
	pkg.resolve = sync.OnceValue(func() error { return nil })
	pkg.setInstallable(parsed, locker.ProjectDir())
	pkg.outputs = outputs{selectedNames: strings.Split(parsed.Outputs, ",")}
	return pkg
}

// resolve is the implementation of Package.resolve, where it is wrapped in a
// sync.OnceValue function. It should not be called directly.
func resolve(pkg *Package) error {
	resolved, err := pkg.lockfile.Resolve(pkg.LockfileKey())
	if err != nil {
		return err
	}
	parsed, err := flake.ParseInstallable(resolved.Resolved)
	if err != nil {
		return err
	}

	// TODO savil. Check with Greg about setting the user-specified outputs
	// somehow here.

	pkg.setInstallable(parsed, pkg.lockfile.ProjectDir())
	return nil
}

func (p *Package) setInstallable(i flake.Installable, projectDir string) {
	if i.Ref.Type == flake.TypePath && !filepath.IsAbs(i.Ref.Path) {
		i.Ref.Path = filepath.Join(projectDir, i.Ref.Path)
	}
	p.installable = i
}

var inputNameRegex = regexp.MustCompile("[^a-zA-Z0-9-]+")

// FlakeInputName generates a name for the input that will be used in the
// generated flake.nix to import this package. This name must be unique in that
// flake so we attach a hash to (quasi) ensure uniqueness.
// Input name will be different from raw package name
func (p *Package) FlakeInputName() string {
	_ = p.resolve()

	result := ""
	switch p.installable.Ref.Type {
	case flake.TypePath:
		result = filepath.Base(p.installable.Ref.Path) + "-" + p.Hash()
	case flake.TypeGitHub:
		isNixOS := strings.ToLower(p.installable.Ref.Owner) == "nixos"
		isNixpkgs := isNixOS && strings.ToLower(p.installable.Ref.Repo) == "nixpkgs"
		if isNixpkgs && p.IsDevboxPackage {
			commitHash := nix.HashFromNixPkgsURL(p.installable.Ref.String())
			result = "nixpkgs-" + commitHash[:min(6, len(commitHash))]
		} else {
			result = "gh-" + p.installable.Ref.Owner + "-" + p.installable.Ref.Repo
			if p.installable.Ref.Rev != "" {
				result += "-" + p.installable.Ref.Rev
			} else if p.installable.Ref.Ref != "" {
				result += "-" + p.installable.Ref.Ref
			}
		}
	default:
		result = p.installable.Ref.String() + "-" + p.Hash()
	}

	// replace all non-alphanumeric with dashes
	return inputNameRegex.ReplaceAllString(result, "-")
}

// URLForFlakeInput returns the input url to be used in a flake.nix file. This
// input can be used to import the package.
func (p *Package) URLForFlakeInput() string {
	if err := p.resolve(); err != nil {
		// TODO(landau): handle error
		panic(err)
	}
	return p.installable.Ref.String()
}

// IsInstallable returns whether this package is installable. Not to be confused
// with the Installable() method which returns the corresponding nix concept.
func (p *Package) IsInstallable() bool {
	return p.isInstallable()
}

func (p *Package) PatchGlibc() bool {
	return p.patchGlibc != nil && p.patchGlibc()
}

// Installables for this package. Installables is a nix concept defined here:
// https://nixos.org/manual/nix/stable/command-ref/new-cli/nix.html#installables
func (p *Package) Installables() ([]string, error) {
	outputNames, err := p.GetOutputNames()
	if err != nil {
		return nil, err
	}
	installables := []string{}
	for _, outputName := range outputNames {
		i, err := p.InstallableForOutput(outputName)
		if err != nil {
			return nil, err
		}
		installables = append(installables, i)
	}
	if len(installables) == 0 {
		// This means that the package is not in the binary cache
		// OR it is a flake (??)
		installable, err := p.urlForInstall()
		if err != nil {
			return nil, err
		}
		return []string{installable}, nil
	}
	return installables, nil
}

func (p *Package) InstallableForOutput(output string) (string, error) {
	inCache, err := p.IsOutputInBinaryCache(output)
	if err != nil {
		return "", err
	}

	if inCache {
		installable, err := p.InputAddressedPathForOutput(output)
		if err != nil {
			return "", err
		}
		return installable, nil
	}

	// TODO savil: does this work for outputs?
	installable, err := p.urlForInstall()
	if err != nil {
		return "", err
	}
	return installable, nil
}

// FlakeInstallable returns a flake installable. The raw string must contain
// a valid flake reference parsable by ParseFlakeRef, optionally followed by an
// #attrpath and/or an ^output.
func (p *Package) FlakeInstallable() (flake.Installable, error) {
	return flake.ParseInstallable(p.Raw)
}

// urlForInstall is used during `nix profile install`.
// The key difference with URLForFlakeInput is that it has a suffix of
// `#attributePath`
func (p *Package) urlForInstall() (string, error) {
	if err := p.resolve(); err != nil {
		return "", err
	}
	return p.installable.String(), nil
}

func (p *Package) NormalizedDevboxPackageReference() (string, error) {
	if err := p.resolve(); err != nil {
		return "", err
	}
	if p.installable.AttrPath == "" {
		return "", nil
	}
	clone := p.installable
	clone.AttrPath = fmt.Sprintf("legacyPackages.%s.%s", nix.System(), clone.AttrPath)
	return clone.String(), nil
}

// PackageAttributePath returns the short attribute path for a package which
// does not include packages/legacyPackages or the system name.
func (p *Package) PackageAttributePath() (string, error) {
	if err := p.resolve(); err != nil {
		return "", err
	}
	return p.installable.AttrPath, nil
}

// FullPackageAttributePath returns the attribute path for a package. It is not
// always normalized which means it should not be used to compare packages.
// During happy paths (devbox packages and nix flakes that contains a fragment)
// it is much faster than NormalizedPackageAttributePath
func (p *Package) FullPackageAttributePath() (string, error) {
	if p.IsDevboxPackage {
		reference, err := p.NormalizedDevboxPackageReference()
		if err != nil {
			return "", err
		}
		_, fragment, _ := strings.Cut(reference, "#")
		return fragment, nil
	}
	return p.NormalizedPackageAttributePath()
}

// NormalizedPackageAttributePath returns an attribute path normalized by nix
// search. This is useful for comparing different attribute paths that may
// point to the same package. Note, it may be an expensive call.
func (p *Package) NormalizedPackageAttributePath() (string, error) {
	if p.normalizedPackageAttributePathCache != "" {
		return p.normalizedPackageAttributePathCache, nil
	}
	path, err := p.normalizePackageAttributePath()
	if err != nil {
		return path, err
	}
	p.normalizedPackageAttributePathCache = path
	return p.normalizedPackageAttributePathCache, nil
}

// normalizePackageAttributePath calls nix search to find the normalized attribute
// path. It may be an expensive call (~100ms).
func (p *Package) normalizePackageAttributePath() (string, error) {
	if err := p.resolve(); err != nil {
		return "", err
	}

	query := p.installable.String()
	if query == "" {
		query = p.Raw
	}

	// We prefer nix.Search over just trying to parse the package's "URL" because
	// nix.Search will guarantee that the package exists for the current system.
	var infos map[string]*nix.Info
	var err error
	if p.IsDevboxPackage && !p.IsRunX() {
		// Perf optimization: For queries of the form nixpkgs/<commit>#foo, we can
		// use a nix.Search cache.
		//
		// This will be slow if its the first time on the user's machine that this
		// query is running. Otherwise, it will be cached and fast.
		if infos, err = nix.SearchNixpkgsAttribute(query); err != nil {
			return "", err
		}
	} else {
		// fallback to the slow but generalized nix.Search
		if infos, err = nix.Search(query); err != nil {
			return "", err
		}
	}

	if len(infos) == 1 {
		return lo.Keys(infos)[0], nil
	}

	// If ambiguous, try to find a default output
	if len(infos) > 1 && p.installable.AttrPath == "" {
		for key := range infos {
			if strings.HasSuffix(key, ".default") {
				return key, nil
			}
		}
		for key := range infos {
			if strings.HasPrefix(key, "defaultPackage.") {
				return key, nil
			}
		}
	}

	// Still ambiguous, return error
	if len(infos) > 1 {
		outputs := fmt.Sprintf("It has %d possible outputs", len(infos))
		if len(infos) < 10 {
			outputs = "It has the following possible outputs: \n" +
				strings.Join(lo.Keys(infos), ", ")
		}
		return "", usererr.New(
			"Package \"%s\" is ambiguous. %s",
			p.Raw,
			outputs,
		)
	}

	if nix.PkgExistsForAnySystem(query) {
		return "", usererr.WithUserMessage(
			ErrCannotBuildPackageOnSystem,
			"Package \"%s\" was found, but we're unable to build it for your system."+
				" You may need to choose another version or write a custom flake.",
			p.Raw,
		)
	}

	return "", usererr.New("Package \"%s\" was not found", p.Raw)
}

var ErrCannotBuildPackageOnSystem = errors.New("unable to build for system")

func (p *Package) Hash() string {
	sum := ""
	if p.installable.Ref.Type == flake.TypePath {
		// For local flakes, use content hash of the flake.nix file to ensure
		// user always gets newest flake.
		sum, _ = cachehash.File(filepath.Join(p.installable.Ref.Path, "flake.nix"))
	}

	if sum == "" {
		sum = cachehash.Bytes([]byte(p.installable.String()))
	}
	return sum[:min(len(sum), 6)]
}

// Equals compares two Packages. This may be an expensive operation since it
// may have to normalize a Package's attribute path, which may require a network
// call.
func (p *Package) Equals(other *Package) bool {
	if p.Raw == other.Raw || p.installable == other.installable {
		return true
	}

	// check inputs without fragments as optimization. Next step is expensive
	if p.URLForFlakeInput() != other.URLForFlakeInput() {
		return false
	}

	name, err := p.NormalizedPackageAttributePath()
	if err != nil {
		return false
	}
	otherName, err := other.NormalizedPackageAttributePath()
	if err != nil {
		return false
	}
	return name == otherName
}

// CanonicalName returns the name of the package without the version
// it only applies to devbox packages
func (p *Package) CanonicalName() string {
	if !p.IsDevboxPackage {
		return ""
	}
	name, _, _ := strings.Cut(p.Raw, "@")
	return name
}

func (p *Package) Versioned() string {
	if p.IsDevboxPackage && !p.isVersioned() {
		return p.Raw + "@latest"
	}
	return p.Raw
}

func (p *Package) IsLegacy() bool {
	return p.IsDevboxPackage && !p.isVersioned() && p.lockfile.Get(p.Raw).GetSource() == ""
}

func (p *Package) LegacyToVersioned() string {
	if !p.IsLegacy() {
		return p.Raw
	}
	return p.Raw + "@latest"
}

// EnsureNixpkgsPrefetched will prefetch flake for the nixpkgs registry for the package.
// This is an internal method, and should not be called directly.
func EnsureNixpkgsPrefetched(ctx context.Context, w io.Writer, pkgs []*Package) error {
	for _, input := range pkgs {
		if err := input.ensureNixpkgsPrefetched(w); err != nil {
			return err
		}
	}
	return nil
}

// ensureNixpkgsPrefetched should be called via the public EnsureNixpkgsPrefetched.
// See function comment there.
func (p *Package) ensureNixpkgsPrefetched(w io.Writer) error {
	inCache, err := p.IsInBinaryCache()
	if err != nil {
		return err
	}
	if inCache {
		// We can skip prefetching nixpkgs, if this package is in the binary
		// cache store.
		return nil
	}

	hash := p.HashFromNixPkgsURL()
	if hash == "" {
		return nil
	}
	return nix.EnsureNixpkgsPrefetched(w, hash)
}

// version returns the version of the package
// it only applies to devbox packages
func (p *Package) version() string {
	if !p.IsDevboxPackage {
		return ""
	}
	_, version, _ := strings.Cut(p.Raw, "@")
	return version
}

func (p *Package) isVersioned() bool {
	return p.IsDevboxPackage && strings.Contains(p.Raw, "@")
}

func (p *Package) HashFromNixPkgsURL() string {
	return nix.HashFromNixPkgsURL(p.URLForFlakeInput())
}

// InputAddressedPath is the input-addressed path in /nix/store
// It is also the key in the BinaryCache for this package
func (p *Package) InputAddressedPaths() ([]string, error) {
	if inCache, err := p.IsInBinaryCache(); err != nil {
		return nil, err
	} else if !inCache {
		return nil,
			errors.Errorf("Package %q cannot be fetched from binary cache store", p.Raw)
	}

	entry, err := p.lockfile.Resolve(p.LockfileKey())
	if err != nil {
		return nil, err
	}

	sysInfo := entry.Systems[nix.System()]
	outputs := sysInfo.DefaultOutputs()

	paths := []string{}
	for _, output := range outputs {
		p, err := p.InputAddressedPathForOutput(output.Name)
		if err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, nil
}

func (p *Package) InputAddressedPathForOutput(output string) (string, error) {
	if inCache, err := p.IsInBinaryCache(); err != nil {
		return "", err
	} else if !inCache {
		return "",
			errors.Errorf("Package %q cannot be fetched from binary cache store", p.Raw)
	}

	entry, err := p.lockfile.Resolve(p.LockfileKey())
	if err != nil {
		return "", err
	}

	sysInfo := entry.Systems[nix.System()]
	for _, out := range sysInfo.Outputs {
		if out.Name == output {
			return out.Path, nil
		}
	}
	return "", errors.Errorf("Output %q not found for package %q", output, p.Raw)
}

func (p *Package) HasAllowInsecure() bool {
	return len(p.AllowInsecure) > 0
}

// StoreName returns the last section of the store path. Example:
// /nix/store/abc123-foo-1.0.0 -> foo-1.0.0
// Warning, this is probably slowish. If you need to call this multiple times,
// consider caching the result.
func (p *Package) StoreName() (string, error) {
	u, err := p.urlForInstall()
	if err != nil {
		return "", err
	}
	name, err := nix.EvalPackageName(u)
	if err != nil {
		return "", err
	}
	return name, nil
}

func (p *Package) EnsureUninstallableIsInLockfile() error {
	// TODO savil: Should !p.isInstallable() be the opposite i.e. p.IsInstallable()?
	// TODO savil: Do we need the IsDevboxPackage check here?
	if !p.IsInstallable() || !p.IsDevboxPackage {
		return nil
	}
	_, err := p.lockfile.Resolve(p.LockfileKey())
	return err
}

func (p *Package) IsRunX() bool {
	return pkgtype.IsRunX(p.Raw)
}

func (p *Package) IsNix() bool {
	return IsNix(p, 0)
}

func (p *Package) RunXPath() string {
	return strings.TrimPrefix(p.Raw, pkgtype.RunXPrefix)
}

func (p *Package) String() string {
	if p.installable.AttrPath != "" {
		return p.installable.AttrPath
	}
	return p.Raw
}

func (p *Package) LockfileKey() string {
	// Use p.Raw instead of p.installable.Ref.String() because that will have
	// absolute paths. TODO: We may want to change SetInstallable to avoid making
	// flake ref absolute.
	return p.Raw
}

func IsNix(p *Package, _ int) bool {
	return !p.IsRunX()
}

func IsRunX(p *Package, _ int) bool {
	return p.IsRunX()
}

func (p *Package) DocsURL() string {
	if p.IsRunX() {
		path, _, _ := strings.Cut(p.RunXPath(), "@")
		return fmt.Sprintf("https://www.github.com/%s", path)
	}
	if p.IsDevboxPackage {
		return fmt.Sprintf("https://www.nixhub.io/packages/%s", p.CanonicalName())
	}
	return ""
}

// GetOutputNames returns the names of the nix package outputs. Outputs can be
// specified in devbox.json package fields or as part of the flake reference.
func (p *Package) GetOutputNames() ([]string, error) {
	if p.IsRunX() {
		return []string{}, nil
	}

	return p.outputs.GetNames(p)
}

// GetOutputsWithCache return outputs and their cache URIs if the package is in the binary cache.
// n+1 WARNING: This will make an http request if FillNarInfoCache is not called before.
// Grep note: this is used in flake template
func (p *Package) GetOutputsWithCache() ([]Output, error) {
	defer debug.FunctionTimer().End()

	names, err := p.GetOutputNames()
	if err != nil || len(names) == 0 {
		return nil, err
	}

	isEligibleForBinaryCache, err := p.isEligibleForBinaryCache()
	if err != nil {
		return nil, err
	}

	outputs := []Output{}
	for _, name := range names {
		output := Output{Name: name}
		if isEligibleForBinaryCache {
			status, err := p.fetchNarInfoStatusOnce(name)
			if err != nil {
				return nil, err
			}
			output.CacheURI = status[name]
		}
		outputs = append(outputs, output)
	}
	return outputs, nil
}

// GetResolvedStorePaths returns the store paths that are resolved (in lockfile)
func (p *Package) GetResolvedStorePaths() ([]string, error) {
	names, err := p.GetOutputNames()
	if err != nil {
		return nil, err
	}
	storePaths := []string{}
	for _, name := range names {
		outputs, err := p.outputsForOutputName(name)
		if err != nil {
			return nil, err
		}
		for _, output := range outputs {
			storePaths = append(storePaths, output.Path)
		}
	}
	return storePaths, nil
}

func (p *Package) GetStorePaths(ctx context.Context, w io.Writer) ([]string, error) {
	storePathsForPackage, err := p.GetResolvedStorePaths()
	if err != nil || len(storePathsForPackage) > 0 {
		return storePathsForPackage, err
	}

	// No fast path, we need to query nix.
	// TODO we should give people the option to add paths to lockfile.
	ux.Fwarning(
		w,
		"Outputs for %s are not in lockfile. Fetching store paths from nix, this may take a while\n",
		p.Raw,
	)

	installables, err := p.Installables()
	if err != nil {
		return nil, err
	}
	for _, installable := range installables {
		storePathsForInstallable, err := nix.StorePathsFromInstallable(
			ctx, installable, p.HasAllowInsecure())
		if err != nil {
			return nil, packageInstallErrorHandler(err, p, installable)
		}
		storePathsForPackage = append(storePathsForPackage, storePathsForInstallable...)
	}
	return storePathsForPackage, nil
}

// packageInstallErrorHandler checks for two kinds of errors to print custom messages for so that Devbox users
// can work around them:
// 1. Packages that cannot be installed on the current system, but may be installable on other systems.packageInstallErrorHandler
// 2. Packages marked insecure by nix
func packageInstallErrorHandler(err error, pkg *Package, installableOrEmpty string) error {
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
