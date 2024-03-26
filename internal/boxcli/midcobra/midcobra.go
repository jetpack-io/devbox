// Copyright 2023 Jetpack Technologies Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package midcobra

import (
	"context"
	"errors"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	"go.jetpack.io/devbox/internal/boxcli/usererr"
	"go.jetpack.io/devbox/internal/cmdutil"
	"go.jetpack.io/devbox/internal/debug"
	"go.jetpack.io/devbox/internal/ux"
)

const DevboxEntrypoint = "DEVBOX_ENTRYPOINT"

type Executable interface {
	AddMiddleware(mids ...Middleware)
	Execute(ctx context.Context, args []string) int
}

type Middleware interface {
	preRun(cmd *cobra.Command, args []string)
	postRun(cmd *cobra.Command, args []string, runErr error)
}

func New(cmd *cobra.Command) Executable {
	return &midcobraExecutable{
		cmd:         cmd,
		middlewares: []Middleware{},
	}
}

type midcobraExecutable struct {
	cmd *cobra.Command

	middlewares []Middleware
}

var _ Executable = (*midcobraExecutable)(nil)

func (ex *midcobraExecutable) AddMiddleware(mids ...Middleware) {
	ex.middlewares = append(ex.middlewares, mids...)
}

func (ex *midcobraExecutable) Execute(ctx context.Context, args []string) int {
	// Ensure cobra uses the same arguments
	ex.cmd.SetContext(ctx)
	_ = ex.cmd.ParseFlags(args)

	if subcmd, _, _ := cmdutil.GetSubcommand(ex.cmd, args); subcmd != nil {
		// Add the DEVBOX_ENTRYPOINT environment variable
		_ = os.Setenv(DevboxEntrypoint, subcmd.Name())
	}

	// Run the 'pre' hooks
	for _, m := range ex.middlewares {
		m.preRun(ex.cmd, args)
	}

	// set args (needed in case caller transforms args in any way)
	ex.cmd.SetArgs(args)

	// Execute the cobra command:
	err := ex.cmd.Execute()

	// Run the 'post' hooks. Note that unlike the default PostRun cobra functionality these
	// run even if the command resulted in an error. This is useful when we still want to clean up
	// before the program exists or we want to log something. The error, if any, gets passed
	// to the post hook.
	for i := len(ex.middlewares) - 1; i >= 0; i-- {
		ex.middlewares[i].postRun(ex.cmd, args, err)
	}

	// Remove the DEVBOX_ENTRYPOINT environment variable
	_ = os.Unsetenv(DevboxEntrypoint)

	if err != nil {
		// If the error is from the exec call, return the exit code of the exec call.
		// Note: order matters! Check if it is a user exec error before a generic exit error.
		var exitErr *exec.ExitError
		var userExecErr *usererr.ExitError
		if errors.As(err, &userExecErr) {
			return userExecErr.ExitCode()
		}
		if errors.As(err, &exitErr) {
			if !debug.IsEnabled() {
				ux.Ferror(ex.cmd.ErrOrStderr(), "There was an internal error. "+
					"Run with DEVBOX_DEBUG=1 for a detailed error message, and consider reporting it at "+
					"https://github.com/jetpack-io/devbox/issues\n")
			}
			return exitErr.ExitCode()
		}
		return 1 // Error exit code
	}
	return 0
}
