// Copyright 2022 Jetpack Technologies Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package nix

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/pkg/errors"
)

func PkgExists(pkg string) bool {
	_, found := PkgInfo(pkg)
	return found
}

type Info struct {
	NixName string
	Name    string
	Version string
	System  string
}

func Exec(path string, command []string) error {
	runCmd := strings.Join(command, " ")
	cmd := exec.Command("nix-shell", path, "--run", runCmd)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return errors.WithStack(cmd.Run())
}

func PkgInfo(pkg string) (*Info, bool) {
	buf := new(bytes.Buffer)
	attr := fmt.Sprintf("nixpkgs.%s", pkg)
	cmd := exec.Command("nix-env", "--json", "-qa", "-A", attr)
	cmd.Stdout = io.MultiWriter(buf, os.Stdout)
	cmd.Stderr = io.MultiWriter(buf, os.Stdout)
	err := cmd.Run()
	if err != nil {
		// nix-env returns an error if the package name is invalid, for now assume
		// all errors are invalid packages.
		return nil, false /* not found */
	}
	pkgInfo := parseInfo(pkg, buf.Bytes())
	if pkgInfo == nil {
		return nil, false /* not found */
	}
	return pkgInfo, true /* found */
}

func parseInfo(pkg string, data []byte) *Info {
	var results map[string]map[string]any
	err := json.Unmarshal(data, &results)
	if err != nil {
		panic(err)
	}
	if len(results) != 1 {
		panic(fmt.Sprintf("unexpected number of results: %d", len(results)))
	}
	for _, result := range results {
		pkgInfo := &Info{
			NixName: pkg,
			Name:    result["pname"].(string),
			Version: result["version"].(string),
			System:  result["system"].(string),
		}
		return pkgInfo
	}
	return nil
}
