// Copyright 2023 Jetpack Technologies Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package boxcli

import (
	"encoding/base64"
	"fmt"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/scrypt"

	"go.jetpack.io/devbox/internal/cloud"
	"go.jetpack.io/devbox/internal/devbox"
	"go.jetpack.io/devbox/internal/devbox/devopt"
	"go.jetpack.io/devbox/internal/devbox/docgen"
	"go.jetpack.io/typeid"
)

type generateCmdFlags struct {
	envFlag           // only used by generate direnv command
	config            configFlags
	force             bool
	printEnvrcContent bool
	githubUsername    string
	rootUser          bool
}

type GenerateReadmeCmdFlags struct {
	generateCmdFlags
	saveTemplate bool
	template     string
}

func generateCmd() *cobra.Command {
	flags := &generateCmdFlags{}

	command := &cobra.Command{
		Use:               "generate",
		Aliases:           []string{"gen"},
		Short:             "Generate supporting files for your project",
		Args:              cobra.MaximumNArgs(0),
		PersistentPreRunE: ensureNixInstalled,
	}
	command.AddCommand(devcontainerCmd())
	command.AddCommand(dockerfileCmd())
	command.AddCommand(debugCmd())
	command.AddCommand(direnvCmd())
	command.AddCommand(genReadmeCmd())
	command.AddCommand(sshConfigCmd())
	command.AddCommand(hashCmd())
	flags.config.register(command)

	return command
}

func debugCmd() *cobra.Command {
	flags := &generateCmdFlags{}
	command := &cobra.Command{
		Use:    "debug",
		Hidden: true,
		Args:   cobra.MaximumNArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGenerateCmd(cmd, flags)
		},
	}
	return command
}

func devcontainerCmd() *cobra.Command {
	flags := &generateCmdFlags{}
	command := &cobra.Command{
		Use:   "devcontainer",
		Short: "Generate Dockerfile and devcontainer.json files under .devcontainer/ directory",
		Long:  "Generate Dockerfile and devcontainer.json files necessary to run VSCode in remote container environments.",
		Args:  cobra.MaximumNArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGenerateCmd(cmd, flags)
		},
	}
	command.Flags().BoolVarP(
		&flags.force, "force", "f", false, "force overwrite on existing files")
	command.Flags().BoolVar(
		&flags.rootUser, "root-user", false, "Use root as default user inside the container")
	return command
}

func dockerfileCmd() *cobra.Command {
	flags := &generateCmdFlags{}
	command := &cobra.Command{
		Use:   "dockerfile",
		Short: "Generate a Dockerfile that replicates devbox shell",
		Long: "Generate a Dockerfile that replicates devbox shell. " +
			"Can be used to run devbox shell environment in an OCI container.",
		Args: cobra.MaximumNArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGenerateCmd(cmd, flags)
		},
	}
	command.Flags().BoolVarP(
		&flags.force, "force", "f", false, "force overwrite existing files")
	command.Flags().BoolVar(
		&flags.rootUser, "root-user", false, "Use root as default user inside the container")
	flags.config.register(command)
	return command
}

func direnvCmd() *cobra.Command {
	flags := &generateCmdFlags{}
	command := &cobra.Command{
		Use:   "direnv",
		Short: "Generate a .envrc file that integrates direnv with this devbox project",
		Long: "Generate a .envrc file that integrates direnv with this devbox project. " +
			"Requires direnv to be installed.",
		Args: cobra.MaximumNArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGenerateDirenvCmd(cmd, flags)
		},
	}
	flags.envFlag.register(command)
	command.Flags().BoolVarP(
		&flags.force, "force", "f", false, "force overwrite existing files")
	command.Flags().BoolVarP(
		&flags.printEnvrcContent, "print-envrc", "p", false, "output contents of devbox configuration to use in .envrc")
	// this command marks a flag as hidden. Error handling for it is not necessary.
	_ = command.Flags().MarkHidden("print-envrc")

	flags.config.register(command)
	return command
}

func sshConfigCmd() *cobra.Command {
	flags := &generateCmdFlags{}
	command := &cobra.Command{
		Use:    "ssh-config",
		Hidden: true,
		Short:  "Generate ssh config to connect to devbox cloud",
		Long:   "Check ssh config and if they don't exist, it generates the configs necessary to connect to devbox cloud VMs.",
		Args:   cobra.MaximumNArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			// ssh-config command is exception and it should run without a config file present
			_, err := cloud.SSHSetup(flags.githubUsername)
			return errors.WithStack(err)
		},
	}
	command.Flags().StringVarP(
		&flags.githubUsername, "username", "u", "", "GitHub username to use for ssh",
	)
	flags.config.register(command)
	return command
}

func genReadmeCmd() *cobra.Command {
	flags := &GenerateReadmeCmdFlags{}

	command := &cobra.Command{
		Use:   "readme [filename]",
		Short: "Generate markdown readme file for this project",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			box, err := devbox.Open(&devopt.Opts{
				Dir:         flags.config.path,
				Environment: flags.config.environment,
				Stderr:      cmd.ErrOrStderr(),
			})
			if err != nil {
				return errors.WithStack(err)
			}
			outPath := ""
			if len(args) > 0 {
				outPath = args[0]
			}
			if flags.saveTemplate {
				return docgen.SaveDefaultReadmeTemplate(outPath)
			}
			return docgen.GenerateReadme(box, outPath, flags.template)
		},
	}
	flags.config.register(command)
	command.Flags().BoolVar(
		&flags.saveTemplate, "save-template", false, "Save default template for the README file")
	command.Flags().StringVarP(
		&flags.template, "template", "t", "", "Path to a custom template for the README file")

	return command
}

func hashCmd() *cobra.Command {
	flags := &generateCmdFlags{}
	command := &cobra.Command{
		Use:   "hash",
		Short: "Generate token",
		Long:  "Generate token",
		Args:  cobra.MaximumNArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHashCmd(cmd, flags)
		},
	}
	return command
}

func runGenerateCmd(cmd *cobra.Command, flags *generateCmdFlags) error {
	// Check the directory exists.
	box, err := devbox.Open(&devopt.Opts{
		Dir:         flags.config.path,
		Environment: flags.config.environment,
		Stderr:      cmd.ErrOrStderr(),
	})
	if err != nil {
		return errors.WithStack(err)
	}
	generateOpts := devopt.GenerateOpts{
		Force:    flags.force,
		RootUser: flags.rootUser,
	}
	switch cmd.Use {
	case "debug":
		return box.Generate(cmd.Context())
	case "devcontainer":
		return box.GenerateDevcontainer(cmd.Context(), generateOpts)
	case "dockerfile":
		return box.GenerateDockerfile(cmd.Context(), generateOpts)
	}
	return nil
}

func runGenerateDirenvCmd(cmd *cobra.Command, flags *generateCmdFlags) error {
	if flags.printEnvrcContent {
		return devbox.PrintEnvrcContent(
			cmd.OutOrStdout(), devopt.EnvFlags(flags.envFlag))
	}

	box, err := devbox.Open(&devopt.Opts{
		Dir:         flags.config.path,
		Environment: flags.config.environment,
		Stderr:      cmd.ErrOrStderr(),
	})
	if err != nil {
		return errors.WithStack(err)
	}

	return box.GenerateEnvrcFile(
		cmd.Context(), flags.force, devopt.EnvFlags(flags.envFlag))
}

func runHashCmd(cmd *cobra.Command, flags *generateCmdFlags) error {
	token, err := typeid.WithPrefix("pat")
	if err != nil {
		return err
	}
	fmt.Println(token.String())
	hash, err := scrypt.Key([]byte(token.String()), []byte(token.String()), 1<<15, 8, 1, 32)
	if err != nil {
		return err
	}
	fmt.Println(base64.StdEncoding.EncodeToString(hash))
	return nil
}
