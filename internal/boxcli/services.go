// Copyright 2024 Jetify Inc. and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package boxcli

import (
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"go.jetpack.io/devbox/internal/devbox"
	"go.jetpack.io/devbox/internal/devbox/devopt"
)

type servicesCmdFlags struct {
	envFlag
	config            configFlags
	runInCurrentShell bool
}

type serviceUpFlags struct {
	background          bool
	processComposeFile  string
	processComposeFlags []string
}

type serviceStopFlags struct {
	allProjects bool
}

func (flags *serviceUpFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&flags.processComposeFile,
		"process-compose-file",
		"",
		"path to process compose file or directory containing process "+
			"compose-file.yaml|yml. Default is directory containing devbox.json",
	)
	cmd.Flags().BoolVarP(
		&flags.background, "background", "b", false, "run service in background")
	cmd.Flags().StringArrayVar(
		&flags.processComposeFlags, "pcflags", []string{}, "pass flags directly to process compose")
}

func (flags *serviceStopFlags) register(cmd *cobra.Command) {
	cmd.Flags().BoolVar(
		&flags.allProjects, "all-projects", false, "stop all running services across all your projects.\nThis flag cannot be used simultaneously with the [services] argument")
}

func servicesCmd(persistentPreRunE ...cobraFunc) *cobra.Command {
	flags := servicesCmdFlags{}
	serviceUpFlags := serviceUpFlags{}
	serviceStopFlags := serviceStopFlags{}
	servicesCommand := &cobra.Command{
		Use:   "services",
		Short: "Interact with devbox services.",
		Long: "Interact with devbox services. Services start in a new shell. " +
			"Plugin services use environment variables specified by plugin unless " +
			"overridden by the user. To override plugin environment variables, use " +
			"the --env or --env-file flag. You may also override in devbox.json by " +
			"using the `env` field or exporting an environment variable in the " +
			"init hook.",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			preruns := append([]cobraFunc{ensureNixInstalled}, persistentPreRunE...)
			for _, fn := range preruns {
				if err := fn(cmd, args); err != nil {
					return err
				}
			}
			return nil
		},
	}

	lsCommand := &cobra.Command{
		Use:   "ls",
		Short: "List available services",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			return listServices(cmd, flags)
		},
	}

	startCommand := &cobra.Command{
		Use:   "start [service]...",
		Short: "Start service. If no service is specified, starts all services",
		RunE: func(cmd *cobra.Command, args []string) error {
			return startServices(cmd, args, flags)
		},
	}

	stopCommand := &cobra.Command{
		Use:     "stop [service]...",
		Aliases: []string{"down"},
		Short:   `Stop one or more services in the current project. If no service is specified, stops all services in the current project.`,
		Long:    `Stop one or more services in the current project. If no service is specified, stops all services in the current project. \nIf the --all-projects flag is specified, stops all running services across all your projects. This flag cannot be used with [service] arguments.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return stopServices(cmd, args, flags, serviceStopFlags)
		},
	}

	restartCommand := &cobra.Command{
		Use:   "restart [service]...",
		Short: "Restart service. If no service is specified, restarts all services",
		RunE: func(cmd *cobra.Command, args []string) error {
			return restartServices(cmd, args, flags)
		},
	}

	upCommand := &cobra.Command{
		Use:   "up [service]...",
		Short: "Starts process manager with specified services. If no services are listed, starts the process manager with all the services in your project",
		RunE: func(cmd *cobra.Command, args []string) error {
			return startProcessManager(cmd, args, flags, serviceUpFlags)
		},
	}

	flags.envFlag.register(servicesCommand)
	flags.config.registerPersistent(servicesCommand)
	servicesCommand.PersistentFlags().BoolVar(
		&flags.runInCurrentShell,
		"run-in-current-shell",
		false,
		"run the command in the current shell instead of a new shell",
	)
	servicesCommand.Flag("run-in-current-shell").Hidden = true
	serviceUpFlags.register(upCommand)
	serviceStopFlags.register(stopCommand)
	servicesCommand.AddCommand(lsCommand)
	servicesCommand.AddCommand(upCommand)
	servicesCommand.AddCommand(restartCommand)
	servicesCommand.AddCommand(startCommand)
	servicesCommand.AddCommand(stopCommand)
	return servicesCommand
}

func listServices(cmd *cobra.Command, flags servicesCmdFlags) error {
	box, err := devbox.Open(&devopt.Opts{
		Dir:         flags.config.path,
		Environment: flags.config.environment,
		Stderr:      cmd.ErrOrStderr(),
	})
	if err != nil {
		return errors.WithStack(err)
	}

	return box.ListServices(cmd.Context(), flags.runInCurrentShell)
}

func startServices(cmd *cobra.Command, services []string, flags servicesCmdFlags) error {
	env, err := flags.Env(flags.config.path)
	if err != nil {
		return err
	}
	box, err := devbox.Open(&devopt.Opts{
		Dir:         flags.config.path,
		Environment: flags.config.environment,
		Env:         env,
		Stderr:      cmd.ErrOrStderr(),
	})
	if err != nil {
		return errors.WithStack(err)
	}

	return box.StartServices(cmd.Context(), flags.runInCurrentShell, services...)
}

func stopServices(
	cmd *cobra.Command,
	services []string,
	servicesFlags servicesCmdFlags,
	flags serviceStopFlags,
) error {
	env, err := servicesFlags.Env(servicesFlags.config.path)
	if err != nil {
		return err
	}
	box, err := devbox.Open(&devopt.Opts{
		Dir:         servicesFlags.config.path,
		Environment: servicesFlags.config.environment,
		Env:         env,
		Stderr:      cmd.ErrOrStderr(),
	})
	if err != nil {
		return errors.WithStack(err)
	}

	if len(services) > 0 && flags.allProjects {
		return errors.New("cannot use both services and --all-projects arguments simultaneously")
	}
	return box.StopServices(
		cmd.Context(), servicesFlags.runInCurrentShell, flags.allProjects, services...)
}

func restartServices(
	cmd *cobra.Command,
	services []string,
	flags servicesCmdFlags,
) error {
	env, err := flags.Env(flags.config.path)
	if err != nil {
		return err
	}
	box, err := devbox.Open(&devopt.Opts{
		Dir:         flags.config.path,
		Environment: flags.config.environment,
		Env:         env,
		Stderr:      cmd.ErrOrStderr(),
	})
	if err != nil {
		return errors.WithStack(err)
	}

	return box.RestartServices(cmd.Context(), flags.runInCurrentShell, services...)
}

func startProcessManager(
	cmd *cobra.Command,
	args []string,
	servicesFlags servicesCmdFlags,
	flags serviceUpFlags,
) error {
	env, err := servicesFlags.Env(servicesFlags.config.path)
	if err != nil {
		return err
	}

	box, err := devbox.Open(&devopt.Opts{
		Dir:                      servicesFlags.config.path,
		Env:                      env,
		Environment:              servicesFlags.config.environment,
		Stderr:                   cmd.ErrOrStderr(),
		CustomProcessComposeFile: flags.processComposeFile,
	})
	if err != nil {
		return errors.WithStack(err)
	}

	return box.StartProcessManager(
		cmd.Context(),
		servicesFlags.runInCurrentShell,
		args,
		devopt.ProcessComposeOpts{
			Background: flags.background,
			ExtraFlags: flags.processComposeFlags,
		},
	)
}
