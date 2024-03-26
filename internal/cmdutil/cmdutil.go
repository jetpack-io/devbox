// Copyright 2023 Jetpack Technologies Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package cmdutil

import (
	"os/exec"
	"sort"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Exists indicates if the command exists
func Exists(command string) bool {
	_, err := exec.LookPath(command)
	return err == nil
}

// GetPathOrDefault gets the path for the given command.
// If it's not found, it will return the given value instead.
func GetPathOrDefault(command, def string) string {
	path, err := exec.LookPath(command)
	if err != nil {
		path = def
	}

	return path
}

func GetSubcommand(cmd *cobra.Command, args []string) (subcmd *cobra.Command, flags []string, err error) {
	if cmd.TraverseChildren {
		subcmd, _, err = cmd.Traverse(args)
	} else {
		subcmd, _, err = cmd.Find(args)
	}

	subcmd.Flags().Visit(func(f *pflag.Flag) {
		flags = append(flags, "--"+f.Name)
	})
	sort.Strings(flags)
	return subcmd, flags, err
}
