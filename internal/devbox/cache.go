package devbox

import (
	"context"
	"errors"
	"io"

	"go.jetpack.io/devbox/internal/boxcli/usererr"
	"go.jetpack.io/devbox/internal/devbox/providers/nixcache"
	"go.jetpack.io/devbox/internal/nix"
	"go.jetpack.io/devbox/internal/ux"
	"go.jetpack.io/pkg/auth"
)

func (d *Devbox) UploadProjectToCache(
	ctx context.Context,
	cacheURI string,
) error {
	if cacheURI == "" {
		var err error
		cacheURI, err = getWriteCacheURI(ctx, d.stderr, d.providers.NixCache)
		if err != nil {
			return err
		}
	}

	creds, err := d.providers.NixCache.Credentials(ctx)
	if err != nil && !errors.Is(err, auth.ErrNotLoggedIn) {
		return err
	}
	profilePath, err := d.profilePath()
	if err != nil {
		return err
	}

	// Ensure state is up to date before uploading to cache.
	// TODO: we may be able to do this more efficiently, not sure everything needs
	// to be installed.
	if err = d.ensureStateIsUpToDate(ctx, ensure); err != nil {
		return err
	}

	return nix.CopyInstallableToCache(ctx, d.stderr, cacheURI, profilePath, creds.Env())
}

func UploadInstallableToCache(
	ctx context.Context,
	stderr io.Writer,
	cacheURI, installable string,
) error {
	if cacheURI == "" {
		var err error
		cacheURI, err = getWriteCacheURI(ctx, stderr, *nixcache.Get())
		if err != nil {
			return err
		}
	}

	creds, err := nixcache.Get().Credentials(ctx)
	if err != nil && !errors.Is(err, auth.ErrNotLoggedIn) {
		return err
	}
	return nix.CopyInstallableToCache(ctx, stderr, cacheURI, installable, creds.Env())
}

func getWriteCacheURI(
	ctx context.Context,
	w io.Writer,
	provider nixcache.Provider,
) (string, error) {
	caches, err := provider.WriteCaches(ctx)
	if err != nil {
		return "", err
	}
	if len(caches) == 0 {
		return "",
			usererr.New("You don't have permission to write to any Nix caches.")
	}
	if len(caches) > 1 {
		ux.Fwarning(w, "Multiple caches available, using %s.\n", caches[0].GetUri())
	}
	return caches[0].GetUri(), nil
}
