// Copyright 2023 Jetpack Technologies Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package nix

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/fatih/color"
	"github.com/pkg/errors"
	"go.jetpack.io/devbox/internal/fileutil"
	"go.jetpack.io/devbox/internal/xdg"
)

// EnsureNixpkgsPrefetched runs the prefetch step to download the flake of the registry
func EnsureNixpkgsPrefetched(w io.Writer, commit string) error {
	// Look up the cached map of commitHash:nixStoreLocation
	commitToLocation, err := nixpkgsCommitFileContents()
	if err != nil {
		return err
	}

	// Check if this nixpkgs.Commit is located in the local /nix/store
	location, isPresent := commitToLocation[commit]
	if isPresent {
		if fi, err := os.Stat(location); err == nil && fi.IsDir() {
			// The nixpkgs for this commit hash is present, so we don't need to prefetch
			return nil
		}
	}

	fmt.Fprintf(w, "Ensuring nixpkgs registry is downloaded.\n")
	cmd := Command(context.TODO(), "flake", "prefetch", FlakeNixpkgs(commit))
	cmd.Stdout = w
	cmd.Stderr = cmd.Stdout
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(w, "Ensuring nixpkgs registry is downloaded: ")
		color.New(color.FgRed).Fprintf(w, "Fail\n")
		return errors.Wrapf(err, "Command: %s", cmd)
	}
	fmt.Fprintf(w, "Ensuring nixpkgs registry is downloaded: ")
	color.New(color.FgGreen).Fprintf(w, "Success\n")

	return saveToNixpkgsCommitFile(commit, commitToLocation)
}

func nixpkgsCommitFileContents() (map[string]string, error) {
	path := nixpkgsCommitFilePath()
	if !fileutil.Exists(path) {
		return map[string]string{}, nil
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	commitToLocation := map[string]string{}
	return commitToLocation, errors.WithStack(json.Unmarshal(contents, &commitToLocation))
}

func saveToNixpkgsCommitFile(commit string, commitToLocation map[string]string) error {
	// Make a query to get the /nix/store path for this commit hash.
	cmd := Command(context.TODO(), "flake", "prefetch", "--json", FlakeNixpkgs(commit))
	out, err := cmd.Output()
	if err != nil {
		return errors.WithStack(err)
	}

	// read the json response
	var prefetchData struct {
		StorePath string `json:"storePath"`
	}
	if err := json.Unmarshal(out, &prefetchData); err != nil {
		return errors.WithStack(err)
	}

	// Ensure the nixpkgs commit file path exists so we can write an update to it
	path := nixpkgsCommitFilePath()
	err = os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil && !errors.Is(err, fs.ErrExist) {
		return errors.WithStack(err)
	}

	// write to the map, jsonify it, and write that json to the nixpkgsCommit file
	commitToLocation[commit] = prefetchData.StorePath
	serialized, err := json.Marshal(commitToLocation)
	if err != nil {
		return errors.WithStack(err)
	}

	return errors.WithStack(os.WriteFile(path, serialized, 0o644))
}

func nixpkgsCommitFilePath() string {
	cacheDir := xdg.CacheSubpath("devbox")
	return filepath.Join(cacheDir, "nixpkgs.json")
}

// IsGithubNixpkgsURL returns true if the package is a flake of the form:
// github:NixOS/nixpkgs/...
//
// While there are many ways to specify this input, devbox always uses
// github:NixOS/nixpkgs/<hash> as the URL. If the user wishes to reference nixpkgs
// themselves, this function may not return true.
func IsGithubNixpkgsURL(url string) bool {
	return strings.HasPrefix(strings.ToLower(url), "github:nixos/nixpkgs/")
}

var hashFromNixPkgsRegex = regexp.MustCompile(`(?i)github:nixos/nixpkgs/([^#]+).*`)

// HashFromNixPkgsURL will (for example) return 5233fd2ba76a3accb5aaa999c00509a11fd0793c
// from github:nixos/nixpkgs/5233fd2ba76a3accb5aaa999c00509a11fd0793c#hello
func HashFromNixPkgsURL(url string) string {
	matches := hashFromNixPkgsRegex.FindStringSubmatch(url)
	if len(matches) == 2 {
		return matches[1]
	}
	return ""
}
