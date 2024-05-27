// Copyright 2024 Jetify Inc. and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package boxcli

import (
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"go.jetpack.io/devbox/internal/devbox"
	"go.jetpack.io/devbox/internal/devbox/devopt"
	"go.jetpack.io/devbox/internal/fileutil"
	"go.jetpack.io/devbox/internal/ux"
	"go.jetpack.io/envsec/pkg/envsec"
)

type secretsFlags struct {
	config configFlags
}

func (f *secretsFlags) envsec(cmd *cobra.Command) (*envsec.Envsec, error) {
	box, err := devbox.Open(&devopt.Opts{
		Dir:         f.config.path,
		Environment: f.config.environment,
		Stderr:      cmd.ErrOrStderr(),
	})
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return box.Secrets(cmd.Context())
}

type secretsInitCmdFlags struct {
	force bool
}

type secretsListFlags struct {
	show   bool
	format string
}

type secretsDownloadFlags struct {
	format string
}

type secretsUploadFlags struct {
	format string
}

func secretsCmd() *cobra.Command {
	flags := &secretsFlags{}
	cmd := &cobra.Command{
		Use:               "secrets",
		Aliases:           []string{"envsec"},
		Short:             "Interact with devbox secrets in jetify cloud.",
		PersistentPreRunE: ensureNixInstalled,
	}
	cmd.AddCommand(secretsDownloadCmd(flags))
	cmd.AddCommand(secretsInitCmd(flags))
	cmd.AddCommand(secretsListCmd(flags))
	cmd.AddCommand(secretsRemoveCmd(flags))
	cmd.AddCommand(secretsSetCmd(flags))
	cmd.AddCommand(secretsUploadCmd(flags))

	flags.config.registerPersistent(cmd)

	return cmd
}

func secretsInitCmd(secretsFlags *secretsFlags) *cobra.Command {
	flags := secretsInitCmdFlags{}
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize secrets management with jetify cloud",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			return secretsInitFunc(cmd, flags, secretsFlags)
		},
	}

	cmd.Flags().BoolVarP(
		&flags.force,
		"force",
		"f",
		false,
		"Force initialization even if already initialized",
	)

	return cmd
}

func secretsSetCmd(flags *secretsFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "set <NAME1>=<value1> [<NAME2>=<value2>]...",
		Short: "Securely store one or more environment variables",
		Long:  "Securely store one or more environment variables. To test contents of a file as a secret use set=@<file>",
		Args:  cobra.MinimumNArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return envsec.ValidateSetArgs(args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			secrets, err := flags.envsec(cmd)
			if err != nil {
				return errors.WithStack(err)
			}

			return secrets.SetFromArgs(cmd.Context(), args)
		},
	}
}

func secretsRemoveCmd(flags *secretsFlags) *cobra.Command {
	return &cobra.Command{
		Use:     "remove <NAME1> [<NAME2>]...",
		Short:   "Remove one or more environment variables",
		Aliases: []string{"rm"},
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			secrets, err := flags.envsec(cmd)
			if err != nil {
				return errors.WithStack(err)
			}

			return secrets.DeleteAll(cmd.Context(), args...)
		},
	}
}

func secretsListCmd(commonFlags *secretsFlags) *cobra.Command {
	flags := secretsListFlags{}
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all secrets",
		Args:    cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			secrets, err := commonFlags.envsec(cmd)
			if err != nil {
				return errors.WithStack(err)
			}

			vars, err := secrets.List(cmd.Context())
			if err != nil {
				return err
			}

			return envsec.PrintEnvVar(
				cmd.OutOrStdout(), secrets.EnvID, vars, flags.show, flags.format)
		},
	}

	cmd.Flags().BoolVarP(
		&flags.show,
		"show",
		"s",
		false,
		"Display secret values in plaintext",
	)
	cmd.Flags().StringVarP(
		&flags.format,
		"format",
		"f",
		"table",
		"Display the key values of each secret in the specified format, one of: table | dotenv | json.",
	)
	return cmd
}

func secretsDownloadCmd(commonFlags *secretsFlags) *cobra.Command {
	flags := secretsDownloadFlags{}
	command := &cobra.Command{
		Use:   "download <file1>",
		Short: "Download environment variables into the specified file",
		Args:  cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return envsec.ValidateFormat(flags.format)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			secrets, err := commonFlags.envsec(cmd)
			if err != nil {
				return errors.WithStack(err)
			}
			if err != nil {
				return errors.WithStack(err)
			}
			absPaths, err := fileutil.EnsureAbsolutePaths(args)
			if err != nil {
				return errors.WithStack(err)
			}
			return secrets.Download(cmd.Context(), absPaths[0], flags.format)
		},
	}

	command.Flags().StringVarP(
		&flags.format, "format", "f", "", "file format: dotenv or json")

	return command
}

func secretsUploadCmd(commonFlags *secretsFlags) *cobra.Command {
	flags := &secretsUploadFlags{}
	command := &cobra.Command{
		Use:   "upload <file1> [<fileN>]...",
		Short: "Upload variables defined in one or more .env files.",
		Args:  cobra.MinimumNArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return envsec.ValidateFormat(flags.format)
		},
		RunE: func(cmd *cobra.Command, paths []string) error {
			secrets, err := commonFlags.envsec(cmd)
			if err != nil {
				return errors.WithStack(err)
			}
			if err != nil {
				return errors.WithStack(err)
			}
			absPaths, err := fileutil.EnsureAbsolutePaths(paths)
			if err != nil {
				return errors.WithStack(err)
			}
			return secrets.Upload(cmd.Context(), absPaths, flags.format)
		},
	}

	command.Flags().StringVarP(
		&flags.format, "format", "f", "", "File format: dotenv or json")

	return command
}

func secretsInitFunc(
	cmd *cobra.Command,
	flags secretsInitCmdFlags,
	secretsFlags *secretsFlags,
) error {
	ctx := cmd.Context()
	box, err := devbox.Open(&devopt.Opts{
		Dir:    secretsFlags.config.path,
		Stderr: cmd.ErrOrStderr(),
	})
	if err != nil {
		return errors.WithStack(err)
	}

	// devbox.Secrets() by default assumes project is initialized (and shows
	// error if not). So we use UninitializedSecrets() here instead.
	secrets := box.UninitializedSecrets(ctx)

	if _, err := secrets.ProjectConfig(); err == nil &&
		!box.Config().IsEnvsecEnabled() {
		// Handle edge case where directory is already set up, but devbox.json is
		// not configured to use jetpack-cloud.
		ux.Finfo(
			cmd.ErrOrStderr(),
			"Secrets already initialized. Adding to devbox config.\n",
		)
	} else if err := secrets.NewProject(ctx, flags.force); err != nil {
		return errors.WithStack(err)
	}
	box.Config().Root.SetStringField("EnvFrom", "jetpack-cloud")
	return box.Config().Root.SaveTo(box.ProjectDir())
}
