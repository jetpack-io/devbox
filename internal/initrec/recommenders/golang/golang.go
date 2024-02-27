// Copyright 2023 Jetpack Technologies Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package golang

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/mod/modfile"

	"go.jetpack.io/devbox/internal/fileutil"
	"go.jetpack.io/devbox/internal/initrec/recommenders"
)

const defaultPkg = "go@1.20" // Default to "latest" for cases where we can't determine a version.

type Recommender struct {
	SrcDir string
}

// implements interface recommenders.Recommender (compile-time check)
var _ recommenders.Recommender = (*Recommender)(nil)

func (r *Recommender) IsRelevant() bool {
	return fileutil.Exists(filepath.Join(r.SrcDir, "go.mod"))
}

func (r *Recommender) Packages() []string {
	goPkg := getGoPackage(r.SrcDir)

	return []string{goPkg}
}

func getGoPackage(srcDir string) string {
	goModPath := filepath.Join(srcDir, "go.mod")
	goVersion := parseGoVersion(goModPath)
	if goVersion != "" {
		return fmt.Sprintf("go@%s", goVersion)
	}
	return defaultPkg
}

func parseGoVersion(gomodPath string) string {
	content, err := os.ReadFile(gomodPath)
	if err != nil {
		return ""
	}
	parsed, err := modfile.ParseLax(gomodPath, content, nil)
	if err != nil {
		return ""
	}
	if parsed.Go == nil {
		return ""
	}
	return parsed.Go.Version
}
