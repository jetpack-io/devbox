// Copyright 2022 Jetpack Technologies Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package boxcli

import (
	"github.com/spf13/cobra"
	"go.jetpack.io/devbox/internal/nix"
)

func SearchCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   "search <query>",
		Short: "Search for packages",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return searchCmdFunc(cmd, args[0])
		},
	}

	return command
}

func searchCmdFunc(cmd *cobra.Command, query string) error {
	results, err := nix.Search(cmd.Context(), query)
	if err != nil {
		return err
	}
	for _, result := range results {
		cmd.Printf("* %s (version: %s)\n", result.Name, result.Version)
	}
	return nil
}
