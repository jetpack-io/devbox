// Copyright 2024 Jetify Inc. and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package boxcli

import (
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"go.jetpack.io/pkg/auth"

	"go.jetpack.io/devbox/internal/devbox"
	"go.jetpack.io/devbox/internal/devbox/devopt"
	"go.jetpack.io/devbox/internal/devbox/providers/identity"
	"go.jetpack.io/devbox/internal/goutil"
)

type pushCmdFlags struct {
	config configFlags
}

func pushCmd() *cobra.Command {
	flags := pushCmdFlags{}
	cmd := &cobra.Command{
		Use: "push <git-repo>",
		Short: "Push a [global] config. Leave empty to use jetify cloud. Can " +
			"be a git repo for self storage.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return pushCmdFunc(cmd, goutil.GetDefaulted(args, 0), flags)
		},
	}

	flags.config.register(cmd)

	return cmd
}

func pushCmdFunc(cmd *cobra.Command, url string, flags pushCmdFlags) error {
	box, err := devbox.Open(&devopt.Opts{
		Dir:         flags.config.path,
		Environment: flags.config.environment,
		Stderr:      cmd.ErrOrStderr(),
	})
	if err != nil {
		return errors.WithStack(err)
	}
	t, err := identity.GenSession(cmd.Context())
	var creds devopt.Credentials
	if err != nil && !errors.Is(err, auth.ErrNotLoggedIn) {
		return errors.WithStack(err)
	} else if t != nil && err == nil {
		creds = devopt.Credentials{
			IDToken: t.IDToken,
			Email:   t.IDClaims().Email,
			Sub:     t.IDClaims().Subject,
		}
	}
	return box.Push(cmd.Context(), devopt.PullboxOpts{
		URL:         url,
		Credentials: creds,
	})
}
