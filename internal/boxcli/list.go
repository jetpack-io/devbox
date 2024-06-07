// Copyright 2024 Jetify Inc. and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package boxcli

import (
	"fmt"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"go.jetpack.io/devbox/internal/devbox"
	"go.jetpack.io/devbox/internal/devbox/devopt"
)

type listCmdFlags struct {
	config configFlags
}

func listCmd() *cobra.Command {
	flags := listCmdFlags{}
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List installed packages",
		PreRunE: ensureNixInstalled,
		RunE: func(cmd *cobra.Command, args []string) error {
			box, err := devbox.Open(&devopt.Opts{
				Dir:    flags.config.path,
				Stderr: cmd.ErrOrStderr(),
			})
			if err != nil {
				return errors.WithStack(err)
			}
			for _, p := range box.AllPackageNamesIncludingRemovedTriggerPackages() {
				fmt.Fprintf(cmd.OutOrStdout(), "* %s\n", p)
			}
			return nil
		},
	}
	flags.config.register(cmd)
	return cmd
}
