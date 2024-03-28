// Copyright 2023 Jetpack Technologies Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package boxcli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"go.jetpack.io/devbox/internal/devbox"
	"go.jetpack.io/devbox/internal/devbox/devopt"
	"go.jetpack.io/devbox/internal/devbox/providers/identity"
)

func authCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Devbox auth commands",
	}

	cmd.AddCommand(loginCmd())
	cmd.AddCommand(logoutCmd())
	cmd.AddCommand(whoAmICmd())

	return cmd
}

func loginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Login to devbox",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := identity.Get().AuthClient()
			if err != nil {
				return err
			}
			t, err := c.LoginFlow()
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Logged in as: %s\n", t.IDClaims().Email)
			return nil
		},
	}

	return cmd
}

func logoutCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Logout from devbox",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := identity.Get().AuthClient()
			if err != nil {
				return err
			}
			err = c.LogoutFlow()
			if err == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "Logged out successfully")
			}
			return err
		},
	}

	return cmd
}

type whoAmICmdFlags struct {
	showTokens bool
}

func whoAmICmd() *cobra.Command {
	flags := &whoAmICmdFlags{}
	cmd := &cobra.Command{
		Use:   "whoami",
		Short: "Show the current user",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			box, err := devbox.Open(&devopt.Opts{Dir: wd, Stderr: cmd.ErrOrStderr()})
			if err != nil {
				return err
			}
			return box.UninitializedSecrets(cmd.Context()).
				WhoAmI(cmd.Context(), cmd.OutOrStdout(), flags.showTokens)
		},
	}

	cmd.Flags().BoolVar(
		&flags.showTokens,
		"show-tokens",
		false,
		"Show the access, id, and refresh tokens",
	)

	return cmd
}
