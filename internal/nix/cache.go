package nix

import (
	"context"
	"fmt"
	"io"
	"os"
)

func CopyInstallableToCache(
	ctx context.Context,
	out io.Writer,
	// Note: installable is a string instead of a flake.Installable
	// because flake.Installable does not support store paths yet. It converts
	// paths into "path" flakes which is not what we want for /nix/store paths.
	// TODO: Add support for store paths in flake.Installable
	to, installable string,
	env []string,
) error {
	fmt.Fprintf(out, "Copying %s to %s\n", installable, to)
	cmd := commandContext(
		ctx,
		"copy", "--to", to,
		// --impure makes NIXPKGS_ALLOW_* environment variables work.
		"--impure",
		// --refresh checks the cache to ensure it is up to date. Otherwise if
		// anything has was copied previously from this machine and then purged
		// it may not be copied again. It's fairly fast, but not instant.
		"--refresh",
		installable,
	)

	cmd.Stdin = os.Stdin
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Env = append(allowUnfreeEnv(allowInsecureEnv(os.Environ())), env...)

	return cmd.Run()
}
