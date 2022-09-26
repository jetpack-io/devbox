// Copyright 2022 Jetpack Technologies Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package boxcli

import (
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"go.jetpack.io/devbox"
	"go.jetpack.io/devbox/boxcli/usererr"
)

func InitCmd() *cobra.Command {
	fixes := &[]string{}

	command := &cobra.Command{
		Use:   "init [<dir>]",
		Short: "Initialize a directory as a devbox project",
		Args:  cobra.MaximumNArgs(1),
		RunE:  initCmdFunc(fixes),
	}
	command.Flags().StringSliceVar(fixes, "fix", nil, "Run a comma-separated list of fixes on a devbox config: 'prompt'")
	return command
}

func initCmdFunc(fixes *[]string) runFunc {
	return func(cmd *cobra.Command, args []string) error {
		path := pathArg(args)

		created, err := devbox.InitConfig(path)
		if err != nil {
			return errors.WithStack(err)
		}

		// New configs come with the latest and greatest fixes already,
		// so no need to apply any.
		if created || len(*fixes) == 0 {
			return nil
		}

		if len(*fixes) > 1 || (*fixes)[0] != "prompt" {
			return usererr.New("The only currently available fix is 'prompt'.")
		}
		box, err := devbox.Open(path)
		if err != nil {
			return err
		}
		return box.Fix()
	}
}
