// Copyright 2022 Jetpack Technologies Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package boxcli

import (
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"go.jetpack.io/devbox"
)

func InitCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   "init [<dir>]",
		Short: "Initialize a directory as a devbox project",
		Long:  "Initialize a directory as a devbox project. This will create an empty devbox.json in the current directory. You can then add packages using `devbox add`",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInitCmd(cmd, args)
		},
	}

	return command
}

func runInitCmd(cmd *cobra.Command, args []string) error {
	path := pathArg(args)

	_, err := devbox.InitConfig(path, cmd.ErrOrStderr())
	if err != nil {
		return errors.WithStack(err)
	}
	box, err := devbox.Open(path, cmd.ErrOrStderr())
	if err != nil {
		return errors.WithStack(err)
	}
	err = box.GenerateEnvrc(false)
	if err != nil {
		return errors.WithStack(err)
	}
	return nil
}
