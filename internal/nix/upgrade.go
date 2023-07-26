// Copyright 2023 Jetpack Technologies Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package nix

import (
	"context"
	"fmt"
	"os"

	"go.jetpack.io/devbox/internal/redact"
	"go.jetpack.io/devbox/internal/ux"
)

func ProfileUpgrade(ProfileDir string, idx int) error {
	cmd := Command(context.TODO(),
		"profile", "upgrade",
		"--profile", ProfileDir,
		fmt.Sprintf("%d", idx),
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
	ux.Finfo(os.Stderr, "Running \"nix flake update\"\n")
	cmd := Command(context.TODO(), "flake", "update", ProfileDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return redact.Errorf(
			"error running \"nix flake update\": %s: %w", out, err)
	}
	return nil
}
