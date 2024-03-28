// Copyright 2024 Jetpack Technologies Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package boxcli

import (
	"github.com/MakeNowJust/heredoc/v2"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"go.jetpack.io/devbox/internal/devbox"
	"go.jetpack.io/devbox/internal/devbox/devopt"
)

type cacheFlags struct {
	pathFlag
	to string
}

func cacheCmd() *cobra.Command {
	flags := cacheFlags{}
	cacheCommand := &cobra.Command{
		Use:               "cache",
		Short:             "Collection of commands to interact with nix cache",
		PersistentPreRunE: ensureNixInstalled,
	}

	uploadCommand := &cobra.Command{
		Use:     "upload [installable]",
		Aliases: []string{"copy"}, // This mimics the nix command
		Short:   "upload specified or nix packages in current project to cache",
		Long: heredoc.Doc(`
			Upload specified nix installable or packages in current project to cache.
			If [installable] is provided, only that installable will be uploaded. 
			Otherwise, all packages in the project will be uploaded.
			To upload to specific cache, use --to flag. Otherwise, a cache from 
			the cache provider will be used, if available.
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return devbox.UploadInstallableToCache(
					cmd.Context(), cmd.ErrOrStderr(), flags.to, args[0],
				)
			}
			box, err := devbox.Open(&devopt.Opts{
				Dir:    flags.path,
				Stderr: cmd.ErrOrStderr(),
			})
			if err != nil {
				return errors.WithStack(err)
			}
			return box.UploadProjectToCache(cmd.Context(), flags.to)
		},
	}

	flags.pathFlag.register(uploadCommand)
	uploadCommand.Flags().StringVar(
		&flags.to, "to", "", "URI of the cache to copy to")

	cacheCommand.AddCommand(uploadCommand)
	cacheCommand.Hidden = true

	return cacheCommand
}
