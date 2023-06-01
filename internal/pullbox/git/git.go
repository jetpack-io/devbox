// Copyright 2023 Jetpack Technologies Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package git

import (
	"os"
	"strings"

	"github.com/pkg/errors"
	"go.jetpack.io/devbox/internal/pullbox/ioutil"
)

func CloneToTmp(repo string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "devbox")
	if err != nil {
		return "", errors.WithStack(err)
	}
	if err := clone(repo, tmpDir); err != nil {
		return "", errors.WithStack(err)
	}
	return tmpDir, nil
}

func IsRepoURL(url string) bool {
	// For now only support ssh
	return strings.HasPrefix(url, "git@") ||
		(strings.HasPrefix(url, "https://") && strings.HasSuffix(url, ".git"))
}

func clone(repo, dir string) error {
	cmd := ioutil.CommandTTY("git", "clone", repo, dir)
	cmd.Dir = dir
	err := cmd.Run()
	return errors.WithStack(err)
}
