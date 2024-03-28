package devbox

import (
	"context"

	"go.jetpack.io/devbox/internal/devbox/providers/nixcache"
	"go.jetpack.io/devbox/internal/nix"
)

func (d *Devbox) CacheCopy(ctx context.Context, cacheURI string) error {
	var err error
	cacheConfig := nixcache.NixCacheConfig{URI: cacheURI}
	if cacheConfig.URI == "" {
		cacheConfig, err = d.providers.NixCache.Config(ctx)
		if err != nil {
			return err
		}
	}
	profilePath, err := d.profilePath()
	if err != nil {
		return err
	}

	return nix.CopyInstallableToCache(ctx, d.stderr, cacheConfig.URI, profilePath)
}
