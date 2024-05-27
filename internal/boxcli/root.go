// Copyright 2024 Jetify Inc. and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package boxcli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"go.jetpack.io/devbox/internal/boxcli/featureflag"
	"go.jetpack.io/devbox/internal/boxcli/midcobra"
	"go.jetpack.io/devbox/internal/cloud/openssh/sshshim"
	"go.jetpack.io/devbox/internal/cmdutil"
	"go.jetpack.io/devbox/internal/debug"
	"go.jetpack.io/devbox/internal/telemetry"
	"go.jetpack.io/devbox/internal/vercheck"
)

type cobraFunc func(cmd *cobra.Command, args []string) error

var (
	debugMiddleware = &midcobra.DebugMiddleware{}
	traceMiddleware = &midcobra.TraceMiddleware{}
)

type rootCmdFlags struct {
	quiet bool
}

func RootCmd() *cobra.Command {
	flags := rootCmdFlags{}
	command := &cobra.Command{
		Use:   "devbox",
		Short: "Instant, easy, predictable development environments",
		// Warning, PersistentPreRunE is not called if a subcommand also declares
		// it. TODO: Figure out a better way to implement this so that subcommands
		// can't accidentally override it.
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if flags.quiet {
				cmd.SetErr(io.Discard)
			}
			vercheck.CheckVersion(cmd.ErrOrStderr(), cmd.CommandPath())
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	// Stable commands
	command.AddCommand(addCmd())
	if featureflag.Auth.Enabled() {
		command.AddCommand(authCmd())
	}
	command.AddCommand(cacheCmd())
	command.AddCommand(createCmd())
	command.AddCommand(secretsCmd())
	command.AddCommand(generateCmd())
	command.AddCommand(globalCmd())
	command.AddCommand(infoCmd())
	command.AddCommand(initCmd())
	command.AddCommand(installCmd())
	command.AddCommand(integrateCmd())
	command.AddCommand(listCmd())
	command.AddCommand(logCmd())
	command.AddCommand(removeCmd())
	command.AddCommand(runCmd())
	command.AddCommand(searchCmd())
	command.AddCommand(servicesCmd())
	command.AddCommand(setupCmd())
	command.AddCommand(shellCmd())
	command.AddCommand(shellEnvCmd())
	command.AddCommand(updateCmd())
	command.AddCommand(versionCmd())
	// Preview commands
	command.AddCommand(cloudCmd())
	// Internal commands
	command.AddCommand(genDocsCmd())

	// Register the "all" command to list all commands, including hidden ones.
	// This makes debugging easier.
	command.AddCommand(&cobra.Command{
		Use:    "all",
		Short:  "List all commands, including hidden ones",
		Hidden: true,
		Run: func(cmd *cobra.Command, args []string) {
			listAllCommands(command, "")
		},
	})

	command.PersistentFlags().BoolVarP(
		&flags.quiet, "quiet", "q", false, "suppresses logs")
	debugMiddleware.AttachToFlag(command.PersistentFlags(), "debug")
	traceMiddleware.AttachToFlag(command.PersistentFlags(), "trace")

	return command
}

func Execute(ctx context.Context, args []string) int {
	defer debug.Recover()
	rootCmd := RootCmd()
	exe := midcobra.New(rootCmd)
	exe.AddMiddleware(traceMiddleware)
	exe.AddMiddleware(midcobra.Telemetry())
	exe.AddMiddleware(debugMiddleware)
	return exe.Execute(ctx, wrapArgsForRun(rootCmd, args))
}

func Main() {
	timer := debug.Timer(strings.Join(os.Args, " "))
	setSystemBinaryPaths()
	ctx := context.Background()
	if strings.HasSuffix(os.Args[0], "ssh") ||
		strings.HasSuffix(os.Args[0], "scp") {
		os.Exit(sshshim.Execute(ctx, os.Args))
	}

	if len(os.Args) > 1 && os.Args[1] == "upload-telemetry" {
		// This subcommand is hidden and only run by devbox itself as a
		// child process. We need to really make sure that we always
		// exit and don't leave orphaned processes laying around.
		time.AfterFunc(5*time.Second, func() {
			os.Exit(0)
		})
		telemetry.Upload()
		return
	}

	code := Execute(ctx, os.Args[1:])
	// Run out here instead of as a middleware so we can capture any time we spend
	// in middlewares as well.
	timer.End()
	os.Exit(code)
}

func listAllCommands(cmd *cobra.Command, indent string) {
	// Print this command's name and description in table format with indentation
	fmt.Printf("%s%-20s%s\n", indent, cmd.Use, cmd.Short)

	// Recursively list child commands with increased indentation
	for _, childCmd := range cmd.Commands() {
		listAllCommands(childCmd, indent+"\t")
	}
}

func setSystemBinaryPaths() {
	if os.Getenv("DEVBOX_SYSTEM_BASH") == "" {
		os.Setenv("DEVBOX_SYSTEM_BASH", cmdutil.GetPathOrDefault("bash", "/bin/bash"))
	}
	if os.Getenv("DEVBOX_SYSTEM_SED") == "" {
		os.Setenv("DEVBOX_SYSTEM_SED", cmdutil.GetPathOrDefault("sed", "/usr/bin/sed"))
	}
}
