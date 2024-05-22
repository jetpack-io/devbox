package devpkg

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"go.jetpack.io/devbox/internal/boxcli/featureflag"
	"go.jetpack.io/devbox/internal/debug"
	"go.jetpack.io/devbox/internal/lock"
	"go.jetpack.io/devbox/internal/nix"
	"golang.org/x/sync/errgroup"
)

// BinaryCache is the store from which to fetch this package's binaries.
// It is used as FromStore in builtins.fetchClosure.
const BinaryCache = "https://cache.nixos.org"

// useDefaultOutput is a special value for the outputName parameter of
// fetchNarInfoStatusOnce, which indicates that the default outputs should be
// used.
const useDefaultOutput = "__default_output__"

func (p *Package) IsOutputInBinaryCache(outputName string) (bool, error) {
	if eligible, err := p.isEligibleForBinaryCache(); err != nil {
		return false, err
	} else if !eligible {
		return false, nil
	}

	return p.fetchNarInfoStatusOnce(outputName)
}

// IsInBinaryCache returns true if the package is in the binary cache.
// ALERT: Callers in a perf-sensitive code path should call FillNarInfoCache
// before calling this function.
func (p *Package) IsInBinaryCache() (bool, error) {
	if eligible, err := p.isEligibleForBinaryCache(); err != nil {
		return false, err
	} else if !eligible {
		return false, nil
	}

	return p.fetchNarInfoStatusOnce(useDefaultOutput)
}

// FillNarInfoCache checks the remote binary cache for the narinfo of each
// package in the list, and caches the result.
// Callers of IsInBinaryCache may call this function first as a perf-optimization.
func FillNarInfoCache(ctx context.Context, packages ...*Package) error {
	defer debug.FunctionTimer().End()
	if !featureflag.RemoveNixpkgs.Enabled() {
		return nil
	}

	eligiblePackages := []*Package{}
	for _, p := range packages {
		// IMPORTANT: isEligibleForBinaryCache will call resolve() which is NOT
		// concurrency safe. Hence, we call it outside of the go-routine.
		isEligible, err := p.isEligibleForBinaryCache()
		// If the package is not eligible or there is an error in determining that, then skip it.
		if isEligible && err == nil {
			eligiblePackages = append(eligiblePackages, p)
		}
	}
	if len(eligiblePackages) == 0 {
		return nil
	}

	// Pre-compute values read in fillNarInfoCache
	// so they can be read from multiple go-routines without locks
	_, err := nix.Version()
	if err != nil {
		return err
	}
	_ = nix.System()

	group, _ := errgroup.WithContext(ctx)
	for _, p := range eligiblePackages {
		pkg := p // copy the loop variable since its used in a closure below
		names, err := pkg.GetOutputNames()
		if err != nil {
			return err
		}

		for _, o := range names {
			output := o
			group.Go(func() error {
				_, err := pkg.fetchNarInfoStatusOnce(output)
				return err
			})
		}
	}
	return group.Wait()
}

// narInfoStatusFnCache contains cached OnceValues functions that return cache
// status for a package. In the future we can remove this cache by caching
// package objects and ensuring packages are shared globally.
var narInfoStatusFnCache = sync.Map{}

// fetchNarInfoStatusOnce is like fetchNarInfoStatus, but will only ever run
// once and cache the result.
func (p *Package) fetchNarInfoStatusOnce(output string) (bool, error) {
	type inCacheFunc func() (bool, error)
	f, ok := narInfoStatusFnCache.Load(p.Raw)
	if !ok {
		f = inCacheFunc(sync.OnceValues(func() (bool, error) { return p.fetchNarInfoStatus(output) }))
		f, _ = narInfoStatusFnCache.LoadOrStore(p.keyForOutput(output), f)
	}
	return f.(inCacheFunc)()
}

func (p *Package) keyForOutput(output string) string {
	if output == useDefaultOutput {
		sysInfo, err := p.sysInfoIfExists()
		// let's be super safe to always avoid empty key.
		if err == nil && sysInfo != nil && len(sysInfo.DefaultOutputs()) > 0 {
			names := make([]string, len(sysInfo.DefaultOutputs()))
			for i, o := range sysInfo.DefaultOutputs() {
				names[i] = o.Name
			}
			slices.Sort(names)
			output = strings.Join(names, ",")
		}
	}

	return fmt.Sprintf("%s^%s", p.Raw, output)
}

// fetchNarInfoStatus fetches the cache status for the package. It returns
// true if cache exists, false otherwise.
// NOTE: This function always performs an HTTP request and should not be called
// more than once per package.
//
// The outputName parameter is the name of the output to check for in the cache.
// If outputName is UseDefaultOutput, the default outputs will be checked.
func (p *Package) fetchNarInfoStatus(outputName string) (bool, error) {
	sysInfo, err := p.sysInfoIfExists()
	if err != nil {
		return false, err
	} else if sysInfo == nil {
		return false, errors.New(
			"sysInfo is nil, but should not be because" +
				" the package is eligible for binary cache",
		)
	}

	var outputs []lock.Output
	if outputName == useDefaultOutput {
		outputs = sysInfo.DefaultOutputs()
	} else {
		out, err := sysInfo.Output(outputName)
		if err != nil {
			return false, err
		}
		outputs = []lock.Output{out}
	}

	outputInCache := map[string]bool{} // key = output name, value = in cache
	for _, output := range outputs {
		pathParts := nix.NewStorePathParts(output.Path)
		reqURL := BinaryCache + "/" + pathParts.Hash + ".narinfo"
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, reqURL, nil)
		if err != nil {
			return false, err
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return false, err
		}
		// read the body fully, and close it to ensure the connection is reused.
		_, _ = io.Copy(io.Discard, res.Body)
		outputInCache[output.Name] = res.StatusCode == 200
		res.Body.Close()
	}

	// If any output is not in the cache, then the package is deemed to be not in the cache.
	for _, inCache := range outputInCache {
		if !inCache {
			return false, nil
		}
	}
	return true, nil
}

// isEligibleForBinaryCache returns true if we have additional metadata about
// the package to query it from the binary cache.
func (p *Package) isEligibleForBinaryCache() (bool, error) {
	// Patched glibc packages are not in the binary cache.
	if p.PatchGlibc() {
		return false, nil
	}
	sysInfo, err := p.sysInfoIfExists()
	if err != nil {
		return false, err
	}
	return sysInfo != nil, nil
}

// sysInfoIfExists returns the system info for the user's system. If the sysInfo
// is missing, then nil is returned
// NOTE: this is called from multiple go-routines and needs to be concurrency safe.
// Hence, we compute nix.Version, nix.System and lockfile.Resolve prior to calling this
// function from within a goroutine.
func (p *Package) sysInfoIfExists() (*lock.SystemInfo, error) {
	if !featureflag.RemoveNixpkgs.Enabled() {
		return nil, nil
	}

	if !p.isVersioned() {
		return nil, nil
	}

	version, err := nix.Version()
	if err != nil {
		return nil, err
	}

	// disable for nix < 2.17
	if !version.AtLeast(nix.Version2_17) {
		return nil, err
	}

	entry, err := p.lockfile.Resolve(p.Raw)
	if err != nil {
		return nil, err
	}

	userSystem := nix.System()

	if entry.Systems == nil {
		return nil, nil
	}

	// Check if the user's system's info is present in the lockfile
	sysInfo, ok := entry.Systems[userSystem]
	if !ok {
		return nil, nil
	}
	return sysInfo, nil
}
