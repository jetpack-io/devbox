// Copyright 2024 Jetify Inc. and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package nix

import (
	"os"
	"os/exec"

	"go.jetpack.io/devbox/internal/redact"
	"go.jetpack.io/devbox/internal/ux"
)

func ProfileUpgrade(ProfileDir, indexOrName string) error {
	cmd := command(
		"profile", "upgrade",
		"--profile", ProfileDir,
		indexOrName,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return redact.Errorf(
			"error running \"nix profile upgrade\": %s: %w", out, err,
		)
	}
	return nil
}

func FlakeUpdate(ProfileDir string) error {
	version, err := Version()
	if err != nil {
		return err
	}
	ux.Finfo(os.Stderr, "Running \"nix flake update\"\n")
	cmd := exec.Command("nix", "flake", "update")
	if version.AtLeast(Version2_19) {
		cmd.Args = append(cmd.Args, "--flake")
	}
	cmd.Args = append(cmd.Args, ProfileDir)
	cmd.Args = append(cmd.Args, ExperimentalFlags()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return redact.Errorf(
			"error running \"nix flake update\": %s: %w", out, err)
	}
	return nil
}
