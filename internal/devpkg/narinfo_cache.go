package devpkg

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/pkg/errors"
	"go.jetpack.io/devbox/internal/boxcli/featureflag"
	"go.jetpack.io/devbox/internal/debug"
	"go.jetpack.io/devbox/internal/devbox/providers/nixcache"
	"go.jetpack.io/devbox/internal/lock"
	"go.jetpack.io/devbox/internal/nix"
	"golang.org/x/sync/errgroup"
)

// binaryCache is the store from which to fetch this package's binaries.
// It is used as FromStore in builtins.fetchClosure.
const binaryCache = "https://cache.nixos.org"

// useDefaultOutputs is a special value for the outputName parameter of
// fetchNarInfoStatusOnce, which indicates that the default outputs should be
// used.
const useDefaultOutputs = "__default_outputs__"

func (p *Package) IsOutputInBinaryCache(outputName string) (bool, error) {
	if eligible, err := p.isEligibleForBinaryCache(); err != nil {
		return false, err
	} else if !eligible {
		return false, nil
	}

	return p.areExpectedOutputsInCacheOnce(outputName)
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

	return p.areExpectedOutputsInCacheOnce(useDefaultOutputs)
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
		outputNames, err := pkg.GetOutputNames()
		if err != nil {
			return err
		}

		for _, outputName := range outputNames {
			name := outputName
			group.Go(func() error {
				_, err := pkg.fetchNarInfoStatusOnce(name)
				return err
			})
		}
	}
	return group.Wait()
}

// areExpectedOutputsInCacheOnce wraps fetchNarInfoStatusOnce and returns true
// if the expected outputs are in the cache.
func (p *Package) areExpectedOutputsInCacheOnce(outputName string) (bool, error) {
	outputToCache, err := p.fetchNarInfoStatusOnce(outputName)
	if err != nil {
		return false, err
	}
	if outputName == useDefaultOutputs {
		outputs, err := p.outputsForOutputName(outputName)
		// If we don't have default outputs, then we can't check if they are in the cache.
		if err != nil || len(outputs) == 0 {
			return false, err
		}
		return len(outputToCache) == len(outputs), nil
	}
	return len(outputToCache) == 1, nil
}

// fetchNarInfoStatusOnce fetches the cache status for the package and output.
// It returns a map of outputs to cache URIs for each cache hit. Missing
// outputs are not returned, and if no outputs are found, an nil map is returned.
//
// This function caches the result of the first call to avoid multiple calls
// even if there are multiple package structs for the same package.
//
// The outputName parameter is the name of the output to check for in the cache.
// If outputName is UseDefaultOutput, all default outputs will be checked.
func (p *Package) fetchNarInfoStatusOnce(
	outputName string,
) (map[string]string, error) {
	ctx := context.TODO()

	outputToCache := map[string]string{}
	caches, err := readCaches(ctx)
	if err != nil {
		return nil, err
	}

	outputs, err := p.outputsForOutputName(outputName)
	if err != nil {
		return nil, err
	}

	for _, output := range outputs {
		pathParts := nix.NewStorePathParts(output.Path)
		hash := pathParts.Hash
		for _, cache := range caches {
			inCache := false
			if strings.HasPrefix(cache, "s3") {
				inCache, err = fetchNarInfoStatusFromS3(ctx, cache, hash)
				if err != nil {
					return nil, err
				}
			} else {
				inCache, err = fetchNarInfoStatusFromHTTP(ctx, cache, hash)
				if err != nil {
					return nil, err
				}
			}
			if inCache {
				// Found it, no need to check more caches.
				outputToCache[output.Name] = cache
				break
			}
		}
	}

	return outputToCache, nil
}

func (p *Package) AreAllOutputsInCache(
	ctx context.Context, w io.Writer, cacheURI string,
) (bool, error) {
	storePaths, err := p.GetStorePaths(ctx, w)
	if err != nil {
		return false, err
	}

	for _, storePath := range storePaths {
		pathParts := nix.NewStorePathParts(storePath)
		hash := pathParts.Hash
		if strings.HasPrefix(cacheURI, "s3") {
			inCache, err := fetchNarInfoStatusFromS3(ctx, cacheURI, hash)
			if err != nil || !inCache {
				return false, err
			}
		} else {
			inCache, err := fetchNarInfoStatusFromHTTP(ctx, cacheURI, hash)
			if err != nil || !inCache {
				return false, err
			}
		}
	}
	return true, nil
}

func (p *Package) outputsForOutputName(output string) ([]lock.Output, error) {
	sysInfo, err := p.sysInfoIfExists()
	if err != nil || sysInfo == nil {
		return nil, err
	}

	var outputs []lock.Output
	if output == useDefaultOutputs {
		outputs = sysInfo.DefaultOutputs()
	} else {
		out, err := sysInfo.Output(output)
		if err != nil {
			return nil, err
		}
		outputs = []lock.Output{out}
	}
	return outputs, nil
}

// isEligibleForBinaryCache returns true if we have additional metadata about
// the package to query it from the binary cache.
func (p *Package) isEligibleForBinaryCache() (bool, error) {
	defer debug.FunctionTimer().End()
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

var narInfoStatusFnCache = sync.Map{}

func fetchNarInfoStatusFromHTTP(
	ctx context.Context,
	uri string,
	hash string,
) (bool, error) {
	key := fmt.Sprintf("%s/%s", uri, hash)
	fetch, _ := narInfoStatusFnCache.LoadOrStore(key, sync.OnceValues(
		func() (bool, error) {
			ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			url := fmt.Sprintf("%s/%s.narinfo", uri, hash)
			req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
			if err != nil {
				return false, err
			}
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				return false, err
			}
			defer res.Body.Close()
			return res.StatusCode == http.StatusOK, nil
		},
	))
	return fetch.(func() (bool, error))()
}

func fetchNarInfoStatusFromS3(
	ctx context.Context,
	uri string,
	hash string,
) (bool, error) {
	key := fmt.Sprintf("%s/%s", uri, hash)
	fetch, _ := narInfoStatusFnCache.LoadOrStore(key, sync.OnceValues(
		func() (bool, error) {
			s3Client, err := nixcache.S3Client(ctx)
			if err != nil {
				return false, err
			}

			bucketURI, err := url.Parse(uri)
			if err != nil {
				return false, errors.WithStack(err)
			}

			_, err = s3Client.GetObject(ctx,
				&s3.GetObjectInput{
					Bucket: aws.String(bucketURI.Hostname()),
					Key:    aws.String(hash + ".narinfo"),
				},
				func(o *s3.Options) {
					if bucketURI.Query().Get("region") != "" {
						o.Region = bucketURI.Query().Get("region")
					}
				},
			)
			return err == nil, nil
		},
	))
	return fetch.(func() (bool, error))()
}

func readCaches(ctx context.Context) ([]string, error) {
	cacheURIs := []string{binaryCache}
	otherCaches, err := nixcache.CachedReadCaches(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range otherCaches {
		cacheURIs = append(cacheURIs, c.GetUri())
	}
	return cacheURIs, nil
}

func ClearNarInfoCache() {
	narInfoStatusFnCache = sync.Map{}
}
