// Copyright 2024 Jetify Inc. and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package boxcli

import (
	"cmp"
	"fmt"
	"regexp"

	"github.com/pkg/errors"
	"github.com/samber/lo"
	"github.com/spf13/cobra"

	"go.jetpack.io/devbox/internal/boxcli/usererr"
	"go.jetpack.io/devbox/internal/cloud"
	"go.jetpack.io/devbox/internal/devbox"
	"go.jetpack.io/devbox/internal/devbox/devopt"
	"go.jetpack.io/devbox/internal/devbox/docgen"
)

type generateCmdFlags struct {
	envFlag           // only used by generate direnv command
	config            configFlags
	force             bool
	printEnvrcContent bool
	githubUsername    string
	rootUser          bool
}

type generateDockerfileCmdFlags struct {
	generateCmdFlags
	forType string
}

type GenerateReadmeCmdFlags struct {
	generateCmdFlags
	saveTemplate bool
	template     string
}

type GenerateAliasCmdFlags struct {
	config   configFlags
	prefix   string
	noPrefix bool
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
	command.AddCommand(genAliasCmd())
	command.AddCommand(devcontainerCmd())
	command.AddCommand(dockerfileCmd())
	command.AddCommand(debugCmd())
	command.AddCommand(direnvCmd())
	command.AddCommand(genReadmeCmd())
	command.AddCommand(sshConfigCmd())
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
	flags := &generateDockerfileCmdFlags{}
	command := &cobra.Command{
		Use:   "dockerfile",
		Short: "Generate a Dockerfile that replicates devbox shell",
		Long: "Generate a Dockerfile that replicates devbox shell. " +
			"Can be used to run devbox shell environment in an OCI container.",
		Args: cobra.MaximumNArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			box, err := devbox.Open(&devopt.Opts{
				Dir:         flags.config.path,
				Environment: flags.config.environment,
				Stderr:      cmd.ErrOrStderr(),
			})
			if err != nil {
				return errors.WithStack(err)
			}
			return box.GenerateDockerfile(cmd.Context(), devopt.GenerateOpts{
				ForType:  flags.forType,
				Force:    flags.force,
				RootUser: flags.rootUser,
			})
		},
	}
	command.Flags().StringVar(
		&flags.forType, "for", "dev",
		"Generate Dockerfile for a specific type of container (dev, prod)")
	command.Flag("for").Hidden = true
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

func genAliasCmd() *cobra.Command {
	flags := &GenerateAliasCmdFlags{}

	command := &cobra.Command{
		Use:   "alias",
		Short: "Generate shell script aliases for this project",
		Long: "Generate shell script aliases for this project. " +
			"Usage is typically `eval \"$(devbox gen alias)\"`.",
		Args: cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			if flags.prefix != "" && flags.noPrefix {
				return usererr.New(
					"Cannot use both --prefix and --no-prefix flags together")
			}
			box, err := devbox.Open(&devopt.Opts{
				Dir:    flags.config.path,
				Stderr: cmd.ErrOrStderr(),
			})
			if err != nil {
				return errors.WithStack(err)
			}
			re := regexp.MustCompile("[^a-zA-Z0-9_-]+")
			prefix := cmp.Or(flags.prefix, box.Config().Root.Name)
			if prefix == "" && !flags.noPrefix {
				return usererr.New(
					"To generate aliases, you must specify a prefix, set a name " +
						"in devbox.json, or use the --no-prefix flag.")
			}
			prefix = re.ReplaceAllString(prefix, "-")
			for _, script := range box.ListScripts() {
				fmt.Fprintf(
					cmd.OutOrStdout(),
					"alias %s%s='devbox -c \"%s\" run %s'\n",
					lo.Ternary(flags.noPrefix, "", prefix+"-"),
					script,
					box.ProjectDir(),
					script,
				)
			}
			return nil
		},
	}
	flags.config.register(command)
	command.Flags().StringVarP(
		&flags.prefix, "prefix", "p", "", "Prefix for the generated aliases")
	command.Flags().BoolVar(
		&flags.noPrefix, "no-prefix", false,
		"Do not use a prefix for the generated aliases")

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
