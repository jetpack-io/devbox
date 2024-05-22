// Copyright 2024 Jetify Inc. and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package boxcli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"go.jetpack.io/devbox/internal/devbox"
	"go.jetpack.io/devbox/internal/devbox/devopt"
)

type shellEnvCmdFlags struct {
	envFlag
	config            configFlags
	install           bool
	noRefreshAlias    bool
	preservePathStack bool
	pure              bool
	recomputeEnv      bool
	runInitHook       bool
}

func shellEnvCmd() *cobra.Command {
	flags := shellEnvCmdFlags{}
	command := &cobra.Command{
		Use:     "shellenv",
		Short:   "Print shell commands that add Devbox packages to your PATH",
		Args:    cobra.ExactArgs(0),
		PreRunE: ensureNixInstalled,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := shellEnvFunc(cmd, flags)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), s)
			if !strings.HasSuffix(os.Getenv("SHELL"), "fish") {
				fmt.Fprintln(cmd.OutOrStdout(), "hash -r")
			}
			return nil
		},
	}

	command.Flags().BoolVar(
		&flags.runInitHook, "init-hook", false, "runs init hook after exporting shell environment")
	command.Flags().BoolVar(
		&flags.install, "install", false, "install packages before exporting shell environment")

	command.Flags().BoolVar(
		&flags.pure, "pure", false, "if this flag is specified, devbox creates an isolated environment inheriting almost no variables from the current environment. A few variables, in particular HOME, USER and DISPLAY, are retained.")
	command.Flags().BoolVar(
		&flags.preservePathStack, "preserve-path-stack", false,
		"preserves existing PATH order if this project's environment is already in PATH. "+
			"Useful if you want to avoid overshadowing another devbox project that is already active")
	_ = command.Flags().MarkHidden("preserve-path-stack")
	command.Flags().BoolVar(
		&flags.noRefreshAlias, "no-refresh-alias", false,
		"by default, devbox will add refresh alias to the environment"+
			"Use this flag to disable this behavior.")
	_ = command.Flags().MarkHidden("no-refresh-alias")

	// Note, `devbox global shellenv` will override the default value to be false
	command.Flags().BoolVarP(
		&flags.recomputeEnv, "recompute", "r", true,
		"Recompute environment if needed",
	)

	flags.config.register(command)
	flags.envFlag.register(command)

	return command
}

func shellEnvFunc(
	cmd *cobra.Command,
	flags shellEnvCmdFlags,
) (string, error) {
	env, err := flags.Env(flags.config.path)
	if err != nil {
		return "", err
	}
	box, err := devbox.Open(&devopt.Opts{
		Dir:               flags.config.path,
		Environment:       flags.config.environment,
		Stderr:            cmd.ErrOrStderr(),
		PreservePathStack: flags.preservePathStack,
		Pure:              flags.pure,
		Env:               env,
	})
	if err != nil {
		return "", err
	}

	if flags.install {
		if err := box.Install(cmd.Context()); err != nil {
			return "", err
		}
	}

	envStr, err := box.EnvExports(cmd.Context(), devopt.EnvExportsOpts{
		DontRecomputeEnvironment: !flags.recomputeEnv,
		NoRefreshAlias:           flags.noRefreshAlias,
		RunHooks:                 flags.runInitHook,
	})
	if err != nil {
		return "", err
	}

	return envStr, nil
}
