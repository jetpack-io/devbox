// Copyright 2023 Jetpack Technologies Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package lock

import (
	"fmt"
	"time"

	"go.jetpack.io/devbox/internal/boxcli/featureflag"
	"go.jetpack.io/devbox/internal/boxcli/usererr"
	"go.jetpack.io/devbox/internal/nix"
	"go.jetpack.io/devbox/internal/searcher"
	"golang.org/x/exp/maps"
)

func (l *File) ResolveToLockedPackage(pkg string) (*Package, error) {
	name, version, _ := searcher.ParseVersionedPackage(pkg)
	if version == "" {
		return nil, usererr.New("No version specified for %q.", name)
	}

	packageVersion, err := searcher.Client().Resolve(name, version)
	if err != nil {
		return nil, nix.ErrPackageNotFound
	}

	sysInfos := map[string]*SystemInfo{}
	if featureflag.RemoveNixpkgs.Enabled() {
		sysInfos = buildLockSystemInfos(*packageVersion)
	}
	packageInfo, err := selectForSystem(*packageVersion)
	if err != nil {
		return nil, err
	}

	if len(packageInfo.AttrPaths) == 0 {
		return nil, fmt.Errorf("no attr paths found for package %q", name)
	}

	return &Package{
		LastModified: time.Unix(int64(packageInfo.LastUpdated), 0).UTC().
			Format(time.RFC3339),
		Resolved: fmt.Sprintf(
			"github:NixOS/nixpkgs/%s#%s",
			packageInfo.CommitHash,
			packageInfo.AttrPaths[0],
		),
		Version: packageInfo.Version,
		Systems: sysInfos,
	}, nil
}

func selectForSystem(pkg searcher.PackageVersion) (searcher.PackageInfo, error) {
	currentSystem, err := nix.System()
	if err != nil {
		return searcher.PackageInfo{}, err
	}
	if pi, ok := pkg.Systems[currentSystem]; ok {
		return pi, nil
	}
	if pi, ok := pkg.Systems["x86_64-linux"]; ok {
		return pi, nil
	}
	if len(pkg.Systems) == 0 {
		return searcher.PackageInfo{},
			fmt.Errorf("no systems found for package %q", pkg.Name)
	}
	return maps.Values(pkg.Systems)[0], nil
}

func buildLockSystemInfos(pkg searcher.PackageVersion) map[string]*SystemInfo {
	sysInfos := map[string]*SystemInfo{}
	for sysName, sysInfo := range pkg.Systems {
		sysInfos[sysName] = &SystemInfo{
			System:       sysName,
			FromHash:     sysInfo.StoreHash,
			StoreName:    sysInfo.StoreName,
			StoreVersion: sysInfo.StoreVersion,
		}
	}
	return sysInfos
}
