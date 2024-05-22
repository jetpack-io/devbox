// Copyright 2024 Jetify Inc. and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package boxcli

import (
	"github.com/spf13/cobra"

	"go.jetpack.io/devbox/internal/boxcli/usererr"
	"go.jetpack.io/devbox/internal/telemetry"
)

func logCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "log <event-name> [<event-specific-args>]",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return doLogCommand(cmd, args)
		},
	}

	return cmd
}

func doLogCommand(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return usererr.New("expect an <event-name> arg for command: %s", cmd.CommandPath())
	}

	switch eventName := args[0]; eventName {
	case "shell-ready":
		if len(args) < 2 {
			return usererr.New("expected a start-time argument for logging the shell-ready event")
		}
		telemetry.Event(telemetry.EventShellReady, telemetry.Metadata{
			EventStart: telemetry.ParseShellStart(args[1]),
		})
	case "shell-interactive":
		if len(args) < 2 {
			return usererr.New("expected a start-time argument for logging the shell-interactive event")
		}
		telemetry.Event(telemetry.EventShellInteractive, telemetry.Metadata{
			EventStart: telemetry.ParseShellStart(args[1]),
		})
	}
	return usererr.New("unrecognized event-name %s for command: %s", args[0], cmd.CommandPath())
}
