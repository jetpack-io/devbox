package nix

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/pkg/errors"
	"go.jetpack.io/devbox/internal/debug"
	"go.jetpack.io/devbox/internal/redact"
)

type BuildArgs struct {
	AllowInsecure     bool
	Env               []string
	ExtraSubstituters []string
	Flags             []string
	Writer            io.Writer
}

func Build(ctx context.Context, args *BuildArgs, installables ...string) error {
	defer debug.FunctionTimer().End()
	// --impure is required for allowUnfreeEnv/allowInsecureEnv to work.
	cmd := commandContext(ctx, "build", "--impure")
	cmd.Args = append(cmd.Args, args.Flags...)
	cmd.Args = append(cmd.Args, installables...)
	// Adding extra substituters only here to be conservative, but this could also
	// be added to ExperimentalFlags() in the future.
	if len(args.ExtraSubstituters) > 0 {
		cmd.Args = append(cmd.Args,
			"--extra-substituters",
			strings.Join(args.ExtraSubstituters, " "),
		)
	}
	cmd.Env = append(allowUnfreeEnv(os.Environ()), args.Env...)
	if args.AllowInsecure {
		debug.Log("Setting Allow-insecure env-var\n")
		cmd.Env = allowInsecureEnv(cmd.Env)
	}

	// If nix build runs as tty, the output is much nicer. If we ever
	// need to change this to our own writers, consider that you may need
	// to implement your own nicer output. --print-build-logs flag may be useful.
	cmd.Stdin = os.Stdin
	cmd.Stdout = args.Writer
	cmd.Stderr = args.Writer

	debug.Log("Running cmd: %s\n", cmd)
	if err := cmd.Run(); err != nil {
		if exitErr := (&exec.ExitError{}); errors.As(err, &exitErr) {
			debug.Log("Nix build exit code: %d, output: %s\n", exitErr.ExitCode(), exitErr.Stderr)
			return redact.Errorf("nix build exit code: %d, output: %s, err: %w",
				redact.Safe(exitErr.ExitCode()),
				exitErr.Stderr,
				err,
			)
		}
		return err
	}
	return nil
}
